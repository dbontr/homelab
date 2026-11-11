package security

import (
    "bytes"
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func migrationTestState(t *testing.T)string{
    t.Helper()
    dir:=t.TempDir()
    if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)}
    auth:=[]byte(`{"version":1,"last_totp_step":18,"failures":0,"locked_until":"0001-01-01T00:00:00Z"}`)
    if err:=os.WriteFile(filepath.Join(dir,"authenticator.json"),auth,0600);err!=nil{t.Fatal(err)}
    return dir
}

func migrationTestReceipt(t *testing.T,dir,target string,parent *string)[]byte{
    t.Helper()
    auth,exists,err:=protectedMigrationFile(filepath.Join(dir,"authenticator.json"),migrationMaxAuth)
    if err!=nil||!exists{t.Fatal("missing test authenticator",err)}
    vault,exists,err:=protectedMigrationFile(filepath.Join(dir,"credentials.json"),migrationMaxVault)
    if err!=nil{t.Fatal(err)}
    receipt:=migrationReceipt{
        Version:1,Target:target,AuthenticatorSHA256:migrationSHA(auth),
        ParentMarkerSHA256:parent,Nonce:strings.Repeat("a",32),
    }
    if exists{
        digest:=migrationSHA(vault)
        receipt.VaultSHA256=&digest
    }
    raw,err:=json.Marshal(receipt)
    if err!=nil{t.Fatal(err)}
    return raw
}

func TestMigrationSharedRustReceiptFixture(t *testing.T){
    raw:=rustAuthorityFixture(t,"migration-receipt.json")
    receipt,err:=migrationReceiptCheck(raw,"rust")
    if err!=nil{t.Fatal(err)}
    if receipt.AuthenticatorSHA256!=strings.Repeat("a",64)||
        receipt.Nonce!=strings.Repeat("b",32)||
        receipt.VaultSHA256!=nil||receipt.ParentMarkerSHA256!=nil{
        t.Fatal("migration receipt diverged from shared Rust schema")
    }
    if _,err:=migrationReceiptCheck(raw,"go");err==nil{
        t.Fatal("shared receipt lost its exact target binding")
    }
}

func TestMigrationCutoverAndRollbackPreserveCurrentReplayAndVault(t *testing.T){
    dir:=migrationTestState(t)
    credential:=[]byte(`{"credential":"encrypted-fixture-placeholder"}`)
    if err:=os.WriteFile(filepath.Join(dir,"credentials.json"),credential,0600);err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"go",nil);err!=nil{t.Fatal("untouched Go startup rejected",err)}
    if err:=AdmitMigration(dir,"rust",nil);err==nil{t.Fatal("Rust switched without approval")}
    initial:=migrationTestReceipt(t,dir,"rust",nil)
    if err:=AdmitMigration(dir,"rust",initial);err!=nil{t.Fatal(err)}
    oldAuth,err:=os.ReadFile(filepath.Join(dir,"authenticator.json"))
    if err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"rust",initial);err!=nil{
        t.Fatal("selected Rust authority could not restart",err)
    }
    // Runtime can advance its replay state after selection.
    newer:=bytes.Replace(oldAuth,[]byte("18"),[]byte("19"),1)
    if err:=os.WriteFile(filepath.Join(dir,"authenticator.json"),newer,0600);err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"rust",initial);err!=nil{t.Fatal("evolving state blocked restart",err)}
    if err:=AdmitMigration(dir,"go",initial);err==nil{t.Fatal("wrong-target receipt allowed rollback")}
    stale:=migrationTestReceipt(t,dir,"go",nil)
    if err:=AdmitMigration(dir,"go",stale);err==nil{t.Fatal("stale parent receipt allowed rollback")}
    marker,previous,err:=migrationMarkerRead(dir)
    if err!=nil||marker.Target!="rust"{t.Fatalf("invalid selection marker: %+v, %v",marker,err)}
    rollback:=migrationTestReceipt(t,dir,"go",previous)
    if err:=AdmitMigration(dir,"go",rollback);err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"go",rollback);err!=nil{t.Fatal("Go restart refused",err)}
    if err:=AdmitMigration(dir,"go",nil);err==nil{t.Fatal("selection marker bypassed without receipt")}
    if err:=AdmitMigration(dir,"rust",initial);err==nil{t.Fatal("old Rust approval was replayed")}
    current,err:=os.ReadFile(filepath.Join(dir,"authenticator.json"))
    if err!=nil||!bytes.Equal(current,newer){t.Fatal("rollback restored stale authentication state")}
    vault,err:=os.ReadFile(filepath.Join(dir,"credentials.json"))
    if err!=nil||!bytes.Equal(vault,credential){t.Fatal("rollback changed encrypted vault")}
    _,parent,err=migrationMarkerRead(dir)
    if err!=nil{t.Fatal(err)}
    forward:=migrationTestReceipt(t,dir,"rust",parent)
    if err:=AdmitMigration(dir,"rust",forward);err!=nil{t.Fatal(err)}
}

func TestMigrationRejectsStaleStateVersionAndMalformedReceipts(t *testing.T){
    dir:=migrationTestState(t)
    valid:=migrationTestReceipt(t,dir,"rust",nil)
    if err:=os.WriteFile(filepath.Join(dir,"authenticator.json"),[]byte("{}"),0600);err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"rust",valid);err==nil{t.Fatal("stale state was accepted")}
    if err:=os.WriteFile(filepath.Join(dir,"authenticator.json"),[]byte(`{"version":1,"last_totp_step":18,"failures":0,"locked_until":"0001-01-01T00:00:00Z"}`),0600);err!=nil{t.Fatal(err)}
    for _,tc:=range []struct{name string;raw []byte}{
        {"unknown-field",append(bytes.TrimSuffix(valid,[]byte("}")),[]byte(`,"extra":1}`)... )},
        {"trailing",append(bytes.Clone(valid),[]byte("{}")...)},
        {"bad-target",bytes.Replace(valid,[]byte(`"rust"`),[]byte(`"go"`),1)},
        {"bad-nonce",bytes.Replace(valid,[]byte(strings.Repeat("a",32)),[]byte("invalid"),1)},
    }{
        t.Run(tc.name,func(t *testing.T){
            if err:=AdmitMigration(dir,"rust",tc.raw);err==nil{t.Fatal("malformed migration receipt accepted")}
        })
    }
    if _,err:=os.Stat(filepath.Join(dir,migrationMarker));!os.IsNotExist(err){
        t.Fatal("invalid receipt published a marker",err)
    }
}

func TestSelectedGoAuthorityCannotRestartWithMissingReplayOrVault(t *testing.T){
    dir:=migrationTestState(t)
    if err:=os.WriteFile(filepath.Join(dir,"credentials.json"),[]byte("{}"),0600);err!=nil{t.Fatal(err)}
    selected:=migrationTestReceipt(t,dir,"go",nil)
    if err:=AdmitMigration(dir,"go",selected);err!=nil{t.Fatal(err)}
    if err:=os.Remove(filepath.Join(dir,"credentials.json"));err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"go",selected);err==nil{
        t.Fatal("selected Go restarted after its encrypted vault disappeared")
    }
    if err:=os.WriteFile(filepath.Join(dir,"credentials.json"),[]byte("{}"),0600);err!=nil{t.Fatal(err)}
    if err:=os.Remove(filepath.Join(dir,"authenticator.json"));err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"go",selected);err==nil{
        t.Fatal("selected Go restarted with empty TOTP replay state")
    }
}

func TestMigrationRejectsSharedOrSymlinkedProtectedRecords(t *testing.T){
    dir:=migrationTestState(t)
    valid:=migrationTestReceipt(t,dir,"rust",nil)
    original:=filepath.Join(dir,"authenticator.json")
    hardlink:=filepath.Join(dir,"shared")
    if err:=os.Link(original,hardlink);err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"rust",valid);err==nil{t.Fatal("hardlinked replay state accepted")}
    if err:=os.Remove(hardlink);err!=nil{t.Fatal(err)}
    if err:=os.Rename(original,hardlink);err!=nil{t.Fatal(err)}
    if err:=os.Symlink(hardlink,original);err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"rust",valid);err==nil{t.Fatal("symlinked replay state accepted")}
    if err:=os.Remove(original);err!=nil{t.Fatal(err)}
    if err:=os.Rename(hardlink,original);err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"rust",valid);err!=nil{t.Fatal(err)}
    marker:=filepath.Join(dir,migrationMarker)
    if err:=os.Rename(marker,marker+".moved");err!=nil{t.Fatal(err)}
    if err:=os.Symlink(marker+".moved",marker);err!=nil{t.Fatal(err)}
    if err:=AdmitMigration(dir,"rust",valid);err==nil{
        t.Fatal("symlinked selection marker was followed")
    }
}
