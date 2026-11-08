package remote

import(
 "os"
 "path/filepath"
 "strings"
 "testing"
 "github.com/dbontr/homelab-new/system/devices"
)

func TestAgentlessInspectionConfiguration(t *testing.T){
 known:=filepath.Join(t.TempDir(),"known_hosts")
 if err:=os.WriteFile(known,[]byte("fpga.example ssh-ed25519 test-key\n"),0600);err!=nil{t.Fatal(err)}
 catalog,err:=devices.New([]devices.Device{{ID:"fpga",Kind:"fpga",Mode:devices.ModeSSH,Connection:"ssh",SSH:&devices.SSHConfig{Host:"fpga.example",User:"research"},AllowedOperations:[]string{"connect","inspect"}}})
 if err!=nil{t.Fatal(err)}
 args,err:=SSHArguments(catalog,"fpga","kernel",known);if err!=nil{t.Fatal(err)}
 joined:=strings.Join(args," ")
 for _,part:=range []string{"StrictHostKeyChecking=yes","ForwardAgent=no","research@fpga.example","uname -a","-F /dev/null"}{
   if !strings.Contains(joined,part){t.Fatalf("missing SSH restriction %q",part)}
 }
 for _,invalid:=range []string{"install","program","sudo","reboot"}{
   if _,err:=SSHArguments(catalog,"fpga",invalid,known);err==nil{t.Fatalf("unrestricted probe accepted: %s",invalid)}
 }
 if _,err:=SSHArguments(catalog,"fpga","kernel","relative_known_hosts");err==nil{t.Fatal("untrusted host-key path accepted")}
}
