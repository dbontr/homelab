// homelab-admin is used on a trusted local terminal, never through an MCP tool.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dbontr/homelab-new/system/security"
)

const ownerSocket = "/run/homelab-securityd/owner.sock"

func secretPrompt(prompt string) (string, error) {
	// systemd-ask-password reads from the owner's terminal/ask-password agent.
	// Neither a password nor an OTP appears in argv, shell history or logs.
	command := exec.Command("systemd-ask-password", "--timeout=90", prompt)
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(string(output), "\n")
	if value == "" {
		return "", errors.New("empty response")
	}
	return value, nil
}

func createProtectedFile(path string, data []byte) error {
	if !filepath.IsAbs(path) {
		return errors.New("credential path must be absolute")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("credential directory must be private")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func enroll(args []string) error {
	f := flag.NewFlagSet("enroll", flag.ContinueOnError)
	owner := f.String("owner", "", "owner identity used by the local security service")
	passwordPath := f.String("password-file", "", "owner password verifier destination")
	totpPath := f.String("totp-file", "", "TOTP key destination")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *owner == "" || *passwordPath == "" || *totpPath == "" || *passwordPath == *totpPath {
		return errors.New("owner and distinct protected credential paths are required")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return errors.New("enrollment requires a trusted interactive terminal")
	}
	defer tty.Close()
	first, err := secretPrompt("Homelab administrative password:")
	if err != nil {
		return err
	}
	second, err := secretPrompt("Confirm administrative password:")
	if err != nil {
		return err
	}
	if first != second {
		return errors.New("passwords differ")
	}
	record, err := security.HashPassphrase([]byte(first))
	if err != nil {
		return err
	}
	key := make([]byte, 20)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
	verifier, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := createProtectedFile(*passwordPath, append(verifier, '\n')); err != nil {
		return err
	}
	if err := createProtectedFile(*totpPath, []byte(secret+"\n")); err != nil {
		_ = os.Remove(*passwordPath)
		return err
	}
	// Display the enrollment secret only on the physically trusted terminal.
	enrollment := url.URL{Scheme: "otpauth", Host: "totp", Path: "Homelab:" + *owner}
	query := url.Values{"secret": {secret}, "issuer": {"Homelab"}, "digits": {"6"}, "period": {"30"}}
	enrollment.RawQuery = query.Encode()
	_, err = fmt.Fprintf(tty, "\nAdd this account to your authenticator (do not copy it into a chat):\n%s\n\n", enrollment.String())
	return err
}

func ownerRequest(ctx context.Context, method, path string, input any) ([]byte, error) {
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", ownerSocket)
	}}
	client := &http.Client{Transport: transport, Timeout: 35 * time.Second}
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, body)
	if err != nil {
		return nil, err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("owner security service unavailable: %w", err)
	}
	defer resp.Body.Close()
	output, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("security service rejected request: HTTP %d", resp.StatusCode)
	}
	return output, nil
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected enroll, vault-init, login, status, pending, confirm, close, credential-add, credential-rotate, credential-list, credential-delete, oauth-list, oauth-begin or oauth-refresh")
	}
	if args[0] == "enroll" {
		return enroll(args[1:])
	}
	if args[0] == "vault-init" {
		flags := flag.NewFlagSet("vault-init", flag.ContinueOnError)
		destination := flags.String("key-file", "", "destination for owner-only credential encryption key")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *destination == "" {
			return errors.New("vault-init requires --key-file in a private directory")
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		defer func() { for i := range key { key[i] = 0 } }()
		return createProtectedFile(*destination, key)
	}
	timeout := 15 * time.Second
	if args[0] == "oauth-refresh" { timeout = 40 * time.Second }
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var path, method string
	var input any
	switch args[0] {
	case "oauth-list":
		if len(args)!=1{return errors.New("oauth-list accepts no arguments")}
		method,path=http.MethodGet,"/v1/oauth/providers"
	case "oauth-begin":
		if len(args)!=2||len(args[1])<1||len(args[1])>64||
			strings.ContainsAny(args[1],"/?#\\ \t\r\n\x00"){
			return errors.New("oauth-begin requires one configured provider name")
		}
		password,err:=secretPrompt("Homelab administrative password:")
		if err!=nil{return err}
		code,err:=secretPrompt("New six-digit authenticator code:")
		if err!=nil{return err}
		method,path=http.MethodPost,"/v1/oauth/"+url.PathEscape(args[1])+"/begin"
		input=map[string]string{"password":password,"totp":code}
	case "oauth-refresh":
		if len(args)!=3||len(args[1])<1||len(args[1])>64||
			strings.ContainsAny(args[1],"/?#\\ \t\r\n\x00")||
			len(args[2])!=32||
			strings.Trim(args[2],"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_")!=""{
			return errors.New("oauth-refresh requires a provider and exact opaque credential handle")
		}
		password,err:=secretPrompt("Homelab administrative password:")
		if err!=nil{return err}
		code,err:=secretPrompt("New six-digit authenticator code:")
		if err!=nil{return err}
		method,path=http.MethodPost,"/v1/oauth/"+url.PathEscape(args[1])+"/refresh"
		input=map[string]any{
			"handle":args[2],"password":password,"totp":code,"confirm":true,
		}
	case "credential-list":
		if len(args) != 1 { return errors.New("credential-list accepts no arguments") }
		method, path = http.MethodGet, "/v1/credentials"
	case "credential-add":
		if len(args) != 2 || args[1] == "" || len(args[1]) > 64 {
			return errors.New("credential-add requires one provider name")
		}
		provider := args[1]
		secret, err := secretPrompt("Provider credential (hidden):")
		if err != nil { return err }
		password, err := secretPrompt("Homelab administrative password:")
		if err != nil { return err }
		code, err := secretPrompt("New six-digit authenticator code:")
		if err != nil { return err }
		method, path = http.MethodPost, "/v1/credentials"
		input = map[string]string{"provider": provider, "secret": secret, "password": password, "totp": code}
	case "credential-rotate":
		if len(args)!=3||args[1]==""||len(args[1])>64||
			len(args[2])!=32||strings.Trim(args[2],"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_")!="" {
			return errors.New("credential-rotate requires provider and exact opaque handle")
		}
		value,err:=secretPrompt("Replacement provider credential (hidden):")
		if err!=nil{return err}
		password,err:=secretPrompt("Homelab administrative password:")
		if err!=nil{return err}
		code,err:=secretPrompt("New six-digit authenticator code:")
		if err!=nil{return err}
		method,path=http.MethodPut,"/v1/credentials/"+args[2]
		input=map[string]string{"provider":args[1],"secret":value,"password":password,"totp":code}
	case "credential-delete":
		if len(args) != 2 || len(args[1]) != 32 || strings.Trim(args[1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
			return errors.New("credential-delete requires an exact opaque credential handle")
		}
		password, err := secretPrompt("Homelab administrative password:")
		if err != nil { return err }
		code, err := secretPrompt("New six-digit authenticator code:")
		if err != nil { return err }
		method, path = http.MethodDelete, "/v1/credentials/" + args[1]
		input = map[string]string{"password": password, "totp": code}
	case "status":
		method, path = http.MethodGet, "/v1/session"
	case "pending":
		method, path = http.MethodGet, "/v1/confirmations"
	case "close":
		method, path = http.MethodDelete, "/v1/session"
	case "login", "confirm":
		if args[0] == "confirm" {
			if len(args) != 2 || len(args[1]) != 48 {
				return errors.New("confirm requires the exact pending challenge ID")
			}
			path = "/v1/confirmations/" + args[1]
		} else {
			path = "/v1/session"
		}
		method = http.MethodPost
		password, err := secretPrompt("Homelab administrative password:")
		if err != nil {
			return err
		}
		code, err := secretPrompt("Six-digit authenticator code:")
		if err != nil {
			return err
		}
		input = map[string]string{"password": password, "totp": code}
	default:
		return errors.New("unknown administrative command")
	}
	response, err := ownerRequest(ctx, method, path, input)
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, bytes.TrimSpace(response), "", "  "); err != nil {
		return err
	}
	fmt.Println(pretty.String())
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "homelab-admin:", err)
		os.Exit(1)
	}
}
