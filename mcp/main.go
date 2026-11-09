// Command homelab-mcp exposes one authenticated MCP endpoint backed by the
// service runtime. It owns no persistent application data.
package main

import (
 "log"
 "os"

 "github.com/dbontr/homelab-new/services/bridge"
)

func main() {
 if err:=bridge.Serve(bridge.MCP,os.Getenv);err!=nil{
  log.Fatal(err)
 }
}
