package security

import (
    "bytes"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "io"
    "os"
    "path/filepath"
    "syscall"
)

// Migration selection is shared with the Rust authority. A receipt is
// prepared while the previous owner is stopped and the shared flock is held.
// Archived credentials and TOTP replay state are never restored on rollback.
type migrationReceipt struct {
    Version int `json:"version"`
    Target string `json:"target"`
    AuthenticatorSHA256 string `json:"authenticator_sha256"`
    VaultSHA256 *string `json:"vault_sha256"`
    ParentMarkerSHA256 *string `json:"parent_marker_sha256"`
    Nonce string `json:"nonce"`
}

type migrationSelection struct {
    Version int `json:"version"`
    Target string `json:"target"`
    ReceiptSHA256 string `json:"receipt_sha256"`
}

const (
    migrationMarker = ".authority-selection.json"
    migrationMaxReceipt = 4096
    migrationMaxAuth = (16 << 10) - 1
    migrationMaxVault = 8 << 20
)

func migrationSHA(raw []byte) string {
    sum:=sha256.Sum256(raw)
    return hex.EncodeToString(sum[:])
}

func validMigrationHex(value string, size int) bool {
    if len(value)!=size*2 { return false }
    for _,c:=range value {
        if (c<'0'||c>'9') && (c<'a'||c>'f'){return false}
    }
    return true
}

func migrationJSON(raw []byte, target any) error {
    // Go's JSON struct decoder accepts case-insensitive field names and
    // duplicate keys. The Rust authority rejects both, so parse the exact
    // field set before decoding a shared migration or replay record.
    var expected []string
    var required []string
    switch target.(type){
    case *migrationReceipt:
        expected=[]string{"version","target","authenticator_sha256",
            "vault_sha256","parent_marker_sha256","nonce"}
        required=[]string{"version","target","authenticator_sha256","nonce"}
    case *migrationSelection:
        expected=[]string{"version","target","receipt_sha256"}
        required=expected
    case *authenticatorState:
        expected=[]string{"version","last_totp_step","failures","locked_until"}
        required=expected
    default:
        return errors.New("unsupported migration record")
    }
    allowed:=make(map[string]bool,len(expected))
    for _,name:=range expected{allowed[name]=true}
    decoder:=json.NewDecoder(bytes.NewReader(raw))
    first,err:=decoder.Token()
    if err!=nil||first!=json.Delim('{'){
        return errors.New("migration record must be an object")
    }
    seen:=make(map[string]bool,len(expected))
    for decoder.More(){
        token,err:=decoder.Token()
        if err!=nil{return err}
        name,ok:=token.(string)
        if !ok||!allowed[name]||seen[name]{
            return errors.New("unknown or repeated migration record field")
        }
        seen[name]=true
        var value json.RawMessage
        if err:=decoder.Decode(&value);err!=nil{return err}
        if bytes.Equal(value,[]byte("null"))&&
            name!="vault_sha256"&&name!="parent_marker_sha256"{
            return errors.New("required migration field cannot be null")
        }
    }
    last,err:=decoder.Token()
    if err!=nil||last!=json.Delim('}'){
        return errors.New("migration object is not closed")
    }
    var remaining any
    if err:=decoder.Decode(&remaining);err!=io.EOF{
        return errors.New("trailing migration data")
    }
    for _,name:=range required{
        if !seen[name]{return errors.New("required migration field missing")}
    }
    strict:=json.NewDecoder(bytes.NewReader(raw))
    strict.DisallowUnknownFields()
    return strict.Decode(target)
}

func protectedMigrationFile(path string, limit int) ([]byte,bool,error) {
    file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
    if errors.Is(err,os.ErrNotExist){return nil,false,nil}
    if err!=nil{return nil,false,errors.New("protected migration file unavailable")}
    defer file.Close()
    info,err:=file.Stat()
    if err!=nil{return nil,false,err}
    inode,ok:=info.Sys().(*syscall.Stat_t)
    if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0077!=0 ||
        inode.Nlink!=1 || inode.Uid!=uint32(os.Geteuid()) {
        return nil,false,errors.New("migration file has unsafe custody")
    }
    raw,err:=io.ReadAll(io.LimitReader(file,int64(limit+1)))
    if err!=nil || len(raw)==0 || len(raw)>limit {
        return nil,false,errors.New("migration file has invalid size")
    }
    return raw,true,nil
}

func migrationReceiptCheck(raw []byte, target string) (migrationReceipt,error) {
    var receipt migrationReceipt
    if len(raw)==0 || len(raw)>migrationMaxReceipt ||
        (target!="go"&&target!="rust") {
        return receipt,errors.New("migration receipt missing or oversized")
    }
    if err:=migrationJSON(raw,&receipt);err!=nil{return receipt,err}
    if receipt.Version!=1 || receipt.Target!=target ||
        !validMigrationHex(receipt.AuthenticatorSHA256,32) ||
        !validMigrationHex(receipt.Nonce,16) ||
        (receipt.VaultSHA256!=nil&&!validMigrationHex(*receipt.VaultSHA256,32)) ||
        (receipt.ParentMarkerSHA256!=nil&&!validMigrationHex(*receipt.ParentMarkerSHA256,32)) {
        return receipt,errors.New("invalid migration receipt format")
    }
    return receipt,nil
}

func currentMigrationState(stateDir string,receipt migrationReceipt) error {
    authenticator,exists,err:=protectedMigrationFile(filepath.Join(stateDir,"authenticator.json"),migrationMaxAuth)
    if err!=nil || !exists || migrationSHA(authenticator)!=receipt.AuthenticatorSHA256{
        return errors.New("authentication state changed after migration preflight")
    }
    vault,exists,err:=protectedMigrationFile(filepath.Join(stateDir,"credentials.json"),migrationMaxVault)
    if err!=nil{return errors.New("encrypted credential state unavailable")}
    if exists!=(receipt.VaultSHA256!=nil) ||
        (exists&&migrationSHA(vault)!=*receipt.VaultSHA256){
        return errors.New("encrypted credential state changed after migration preflight")
    }
    return nil
}

func migrationMarkerRead(stateDir string)(*migrationSelection,*string,error){
    raw,exists,err:=protectedMigrationFile(filepath.Join(stateDir,migrationMarker),migrationMaxReceipt)
    if err!=nil{return nil,nil,err}
    if !exists{return nil,nil,nil}
    var marker migrationSelection
    if err:=migrationJSON(raw,&marker);err!=nil{return nil,nil,err}
    if marker.Version!=1 || (marker.Target!="go"&&marker.Target!="rust") ||
        !validMigrationHex(marker.ReceiptSHA256,32) {
        return nil,nil,errors.New("invalid security authority selection marker")
    }
    fingerprint:=migrationSHA(raw)
    return &marker,&fingerprint,nil
}

func publishMigrationMarker(stateDir string,marker migrationSelection)error{
    encoded,err:=json.Marshal(marker)
    if err!=nil{return err}
    file,err:=os.CreateTemp(stateDir,".selection-*")
    if err!=nil{return err}
    name:=file.Name()
    defer os.Remove(name)
    if err:=file.Chmod(0600);err!=nil{file.Close();return err}
    if _,err:=file.Write(encoded);err!=nil{file.Close();return err}
    if err:=file.Sync();err!=nil{file.Close();return err}
    if err:=file.Close();err!=nil{return err}
    if err:=os.Rename(name,filepath.Join(stateDir,migrationMarker));err!=nil{return err}
    dir,err:=os.Open(stateDir)
    if err!=nil{return err}
    defer dir.Close()
    return dir.Sync()
}

// AdmitMigration must run under the caller's lifetime-long .securityd.lock
// after the target implementation has validated its current version-one
// authentication and encrypted credential records, but before listeners open.
// On a matching same-implementation restart, state hashes can advance;
// transitions require a new parent-bound receipt matching current bytes.
func AdmitMigration(stateDir,target string,receiptBytes []byte)error{
    if !filepath.IsAbs(stateDir)||filepath.Clean(stateDir)!=stateDir ||
        (target!="go"&&target!="rust"){
        return errors.New("invalid migration state owner")
    }
    previous,parent,err:=migrationMarkerRead(stateDir)
    if err!=nil{return err}
    if len(receiptBytes)==0{
        if target=="go"&&previous==nil{return nil}
        return errors.New("authority transition requires an approved receipt")
    }
    receipt,err:=migrationReceiptCheck(receiptBytes,target)
    if err!=nil{return err}
    fingerprint:=migrationSHA(receiptBytes)
    if previous!=nil&&previous.Target==target&&previous.ReceiptSHA256==fingerprint{
        // Repeated startup may observe a newer valid state, but must never
        // initialize a fresh replay history or discard the protected vault.
        auth,exists,err:=protectedMigrationFile(
            filepath.Join(stateDir,"authenticator.json"),migrationMaxAuth)
        if err!=nil||!exists{return errors.New("selected authority lost replay state")}
        var replay authenticatorState
        if err:=migrationJSON(auth,&replay);err!=nil||
            replay.Version!=1||replay.LastTOTPStep < -1||
            replay.Failures<0||replay.Failures>4{
            return errors.New("selected authority has incompatible replay state")
        }
        if receipt.VaultSHA256!=nil{
            _,present,err:=protectedMigrationFile(
                filepath.Join(stateDir,"credentials.json"),migrationMaxVault)
            if err!=nil||!present{
                return errors.New("selected authority lost encrypted vault")
            }
        }
        return nil
    }
    if (receipt.ParentMarkerSHA256==nil)!=(parent==nil){
        return errors.New("migration receipt references another state generation")
    }
    if parent!=nil&&*parent!=*receipt.ParentMarkerSHA256{
        return errors.New("migration receipt parent state does not match")
    }
    if err:=currentMigrationState(stateDir,receipt);err!=nil{return err}
    return publishMigrationMarker(stateDir,migrationSelection{
        Version:1,Target:target,ReceiptSHA256:fingerprint,
    })
}
