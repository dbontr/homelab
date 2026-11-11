package security

import (
    "bytes"
    "crypto/sha256"
    "crypto/tls"
    "encoding/hex"
    "encoding/json"
    "errors"
    "net"
    "net/http"
    "regexp"
    "sort"
    "strings"
    "time"

    "github.com/dbontr/homelab-new/system/canonicaljson"
)

// ServiceCredentialGrant binds a protected credential to one TLS service
// identity and an explicit privileged operation and target. A configured
// argument digest narrows the permitted request to one canonical JSON value.
type ServiceCredentialGrant struct {
    ClientDNS string `json:"client_dns"`
    Alias string `json:"alias"`
    Provider string `json:"provider"`
    Handle string `json:"handle"`
    Operation string `json:"operation"`
    Target string `json:"target"`
    ArgumentsSHA256 string `json:"arguments_sha256,omitempty"`
    Revision string `json:"revision,omitempty"`
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CredentialTicket can be redeemed only by the authenticated service
// identity for the same operation, target, canonical arguments and owner session.
type CredentialTicket struct {
    Alias string `json:"alias"`
    Ticket string `json:"ticket"`
}

type issuedCredentialTicket struct {
    PeerDNS string
    Principal string
    SessionID string
    Alias string
    Operation string
    Target string
    ArgumentsSHA256 string
    Revision string
    Expires time.Time
}

const (
    maxCredentialTickets = 128
    maxCredentialTicketsPerOperation = 8
    credentialTicketLifetime = 60 * time.Second
)

var serviceCredentialName=regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
var serviceCredentialHandle=regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)
var serviceCredentialDigest=regexp.MustCompile(`^[0-9a-f]{64}$`)
var serviceCredentialTicket=regexp.MustCompile(`^[0-9a-f]{48}$`)

// ValidServiceDNS accepts one canonical ASCII host identity, never a wildcard,
// URL, IP literal or ambiguous certificate name.
func ValidServiceDNS(name string) bool {
    if name=="" || len(name)>253 || net.ParseIP(name)!=nil { return false }
    for _,label:=range strings.Split(name,".") {
        if len(label)==0 || len(label)>63 { return false }
        for i:=0;i<len(label);i++ {
            c:=label[i]
            alpha:=c>='a' && c<='z'
            digit:=c>='0' && c<='9'
            if !alpha && !digit && (c!='-' || i==0 || i==len(label)-1) { return false }
        }
    }
    return true
}

func serviceCredentialKey(identity,alias,operation,target string) string {
    return strings.Join([]string{identity,alias,operation,target},"\x00")
}

func (a *AdminSessions) SetServiceCredentialGrants(grants []ServiceCredentialGrant) error {
    if a==nil || len(grants)>128 { return errors.New("invalid service credential policy") }
    a.mu.Lock()
    defer a.mu.Unlock()
    if a.vault==nil { return errors.New("service credentials require the independent vault") }
    if a.serviceCredentialGrants!=nil { return errors.New("service credential policy is already configured") }
    policy:=make(map[string]ServiceCredentialGrant,len(grants))
    for _,grant:=range grants {
        if !ValidServiceDNS(grant.ClientDNS) ||
            !serviceCredentialName.MatchString(grant.Alias) ||
            !serviceCredentialName.MatchString(grant.Provider) ||
            !serviceCredentialHandle.MatchString(grant.Handle) ||
            !allowedIdentifier.MatchString(grant.Operation) ||
            !allowedIdentifier.MatchString(grant.Target) ||
            (grant.ArgumentsSHA256!="" && !serviceCredentialDigest.MatchString(grant.ArgumentsSHA256)) {
            return errors.New("invalid operation-bound credential grant")
        }
        rule,ok:=a.rules[grant.Operation]
        if !ok { return errors.New("credential grant has no authorized operation") }
        validTarget:=false
        for _,target:=range rule.Targets { if target==grant.Target { validTarget=true;break } }
        if !validTarget { return errors.New("credential grant has no authorized target") }
        key:=serviceCredentialKey(grant.ClientDNS,grant.Alias,grant.Operation,grant.Target)
        if _,exists:=policy[key];exists { return errors.New("duplicate operation-bound credential grant") }
        revision,err:=a.vault.Version(grant.Handle,grant.Provider)
        if err!=nil { return errors.New("service credential handle does not exist in the owner vault") }
        if grant.Revision!="" {
            if !serviceCredentialDigest.MatchString(grant.Revision) || grant.Revision!=revision {
                return errors.New("service credential grant revision is invalid or stale")
            }
        } else {
            grant.Revision=revision
        }
        if grant.ExpiresAt!=nil && !grant.ExpiresAt.After(a.now().UTC()) {
            return errors.New("service credential grant has expired")
        }
        policy[key]=grant
    }
    a.serviceCredentialGrants=policy
    return nil
}

func verifiedServiceDNS(state *tls.ConnectionState)(string,bool) {
    if state==nil || len(state.VerifiedChains)==0 || len(state.PeerCertificates)==0 { return "",false }
    peer:=state.PeerCertificates[0]
    if peer==nil || len(peer.DNSNames)!=1 || len(peer.IPAddresses)!=0 ||
        len(peer.EmailAddresses)!=0 || len(peer.URIs)!=0 { return "",false }
    dns:=peer.DNSNames[0]
    if !ValidServiceDNS(dns) || peer.VerifyHostname(dns)!=nil { return "",false }
    return dns,true
}

func verifiedServicePeer(request *http.Request)(string,bool) {
    return verifiedServiceDNS(request.TLS)
}

func (a *AdminSessions) pruneCredentialTickets(now time.Time) {
    for id,ticket:=range a.serviceCredentialTickets {
        if !now.Before(ticket.Expires) { delete(a.serviceCredentialTickets,id) }
    }
}

// issueServiceTickets is called only after an independently verified,
// consuming owner authorization for exactly this operation and resource.
func (a *AdminSessions) issueServiceTickets(principal,peer,operation,target string,args json.RawMessage) ([]CredentialTicket,error) {
    canonical,_,err:=canonicaljson.Object(args)
    if err!=nil { return nil,errors.New("invalid credential authorization arguments") }
    digest:=sha256.Sum256(canonical)
    argumentsSHA:=hex.EncodeToString(digest[:])
    a.mu.Lock()
    defer a.mu.Unlock()
    now:=a.now().UTC()
    if !ValidServiceDNS(peer) || !a.validSession(now) || principal=="" ||
        a.session.principal!=principal || a.serviceCredentialGrants==nil {
        return nil,errors.New("owner session or credential policy unavailable")
    }
    a.pruneCredentialTickets(now)
    grants:=make([]ServiceCredentialGrant,0)
    for _,grant:=range a.serviceCredentialGrants {
        if grant.ClientDNS==peer && grant.Operation==operation && grant.Target==target &&
            (grant.ArgumentsSHA256=="" || grant.ArgumentsSHA256==argumentsSHA) &&
            (grant.ExpiresAt==nil || now.Before(*grant.ExpiresAt)) {
            grants=append(grants,grant)
        }
    }
    if len(grants)>maxCredentialTicketsPerOperation ||
        len(a.serviceCredentialTickets)+len(grants)>maxCredentialTickets {
        return nil,errors.New("credential capability capacity exceeded")
    }
    sort.Slice(grants,func(i,j int)bool{return grants[i].Alias<grants[j].Alias})
    expires:=now.Add(credentialTicketLifetime)
    if idle:=a.session.lastActivity.Add(SessionIdleTimeout);expires.After(idle){expires=idle}
    if absolute:=a.session.created.Add(SessionAbsoluteTimeout);expires.After(absolute){expires=absolute}
    if !now.Before(expires){return nil,errors.New("owner session expired")}
    issued:=make([]CredentialTicket,0,len(grants))
    for _,grant:=range grants {
        id,err:=randomID()
        if err!=nil {
            for _,prior:=range issued {delete(a.serviceCredentialTickets,prior.Ticket)}
            return nil,err
        }
        deadline:=expires
        if grant.ExpiresAt!=nil && deadline.After(*grant.ExpiresAt){deadline=*grant.ExpiresAt}
        a.serviceCredentialTickets[id]=issuedCredentialTicket{
            PeerDNS:peer,Principal:principal,SessionID:a.session.id,Alias:grant.Alias,
            Operation:operation,Target:target,ArgumentsSHA256:argumentsSHA,
            Revision:grant.Revision,Expires:deadline,
        }
        issued=append(issued,CredentialTicket{Alias:grant.Alias,Ticket:id})
    }
    return issued,nil
}

// redeemServiceTicket consumes the ticket before evaluating its requested
// context. Neither an error nor a transport retry can disclose the secret again.
func (a *AdminSessions) redeemServiceTicket(peer,alias,ticket,operation,target string,args json.RawMessage) ([]byte,error) {
    if !serviceCredentialTicket.MatchString(ticket) {return nil,errors.New("invalid capability")}
    canonical,_,err:=canonicaljson.Object(args)
    if err!=nil {return nil,errors.New("invalid capability arguments")}
    digest:=sha256.Sum256(canonical)
    argumentsSHA:=hex.EncodeToString(digest[:])
    a.mu.Lock()
    defer a.mu.Unlock()
    now:=a.now().UTC()
    if !a.validSession(now){return nil,errors.New("owner session expired")}
    a.pruneCredentialTickets(now)
    issued,exists:=a.serviceCredentialTickets[ticket]
    delete(a.serviceCredentialTickets,ticket)
    if !exists || !now.Before(issued.Expires) ||
        issued.PeerDNS!=peer || issued.Alias!=alias ||
        issued.Operation!=operation || issued.Target!=target ||
        issued.ArgumentsSHA256!=argumentsSHA ||
        issued.Principal!=a.session.principal || issued.SessionID!=a.session.id {
        return nil,errors.New("credential capability not authorized")
    }
    grant,ok:=a.serviceCredentialGrants[serviceCredentialKey(peer,alias,operation,target)]
    if !ok || grant.Revision!=issued.Revision ||
        (grant.ArgumentsSHA256!="" && grant.ArgumentsSHA256!=argumentsSHA) ||
        (grant.ExpiresAt!=nil && !now.Before(*grant.ExpiresAt)) || a.vault==nil {
        return nil,errors.New("credential grant revoked or expired")
    }
    var secret []byte
    err=a.vault.WithCredentialVersion(grant.Handle,grant.Provider,issued.Revision,func(raw []byte)error {
        if len(raw)==0 || len(raw)>32768 {return errors.New("invalid protected credential")}
        secret=bytes.Clone(raw)
        return nil
    })
    if err!=nil {return nil,errors.New("credential unavailable or rotated")}
    return secret,nil
}

// The credential route exists only on the private mutually authenticated
// listener. A valid alias or certificate alone never authorizes a read.
func (a *AdminSessions) ServiceCredentialHandler() http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        w.Header().Set("Cache-Control","no-store")
        w.Header().Set("Pragma","no-cache")
        w.Header().Set("X-Content-Type-Options","nosniff")
        w.Header().Set("Referrer-Policy","no-referrer")
        alias:=r.PathValue("alias")
        identity,verified:=verifiedServicePeer(r)
        if r.Method!=http.MethodPost || !verified || !serviceCredentialName.MatchString(alias) {
            http.Error(w,"service credential access denied",http.StatusForbidden)
            return
        }
        r.Body=http.MaxBytesReader(w,r.Body,64<<10)
        var input struct{
            Ticket string `json:"ticket"`
            Operation string `json:"operation"`
            Target string `json:"target"`
            Arguments json.RawMessage `json:"arguments"`
        }
        if err:=readJSON(r,&input);err!=nil || len(input.Arguments)==0 {
            http.Error(w,"invalid credential redemption",http.StatusBadRequest)
            return
        }
        secret,err:=a.redeemServiceTicket(identity,alias,input.Ticket,
            input.Operation,input.Target,input.Arguments)
        if err!=nil {
            http.Error(w,"service credential access denied",http.StatusForbidden)
            return
        }
        defer func(){for i:=range secret{secret[i]=0}}()
        w.Header().Set("Content-Type","application/octet-stream")
        w.WriteHeader(http.StatusOK)
        _,_=w.Write(secret)
    })
}
