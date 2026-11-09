// Command homelab-api exposes an optional, bounded native HTTP API.
// All domain state remains under the owning service.
package main

import (
 "log"
 "os"

 "github.com/dbontr/homelab-new/services/bridge"
)

func main() {
 if err:=bridge.Serve(bridge.API,os.Getenv);err!=nil{
  log.Fatal(err)
 }
}
