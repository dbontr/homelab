// Check the repository reference ledger without external dependencies.
package main

import (
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "net/url"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "unicode/utf8"
)

type issue struct{
 Code string `json:"code"`
 Line int `json:"line"`
 Message string `json:"message"`
}

var entry=regexp.MustCompile(`^\s*[-*]\s+\[([^\]]+)\]\((https?://[^)]+)\)\s+[—-]\s+(.+)$`)
var local=regexp.MustCompile(`(?i)(sandbox:|file:/{1,3}|/mnt/data/|/home/[^/]+/|[A-Za-z]:\\Users\\)`)
var ephemeral=regexp.MustCompile(`(?i)(chatgpt\.com/(share|c)/|claude\.ai/chat|gemini\.google\.com/app|copilot\.microsoft\.com/(chats?|share)|perplexity\.ai/search)`)

func repoRoot(path string)(string,error){
 absolute,err:=filepath.Abs(path)
 if err!=nil{return "",err}
 if info,err:=os.Stat(absolute);err==nil&&!info.IsDir(){absolute=filepath.Dir(absolute)}
 for current:=absolute;;current=filepath.Dir(current){
  if _,err:=os.Stat(filepath.Join(current,".git"));err==nil{return current,nil}
  if filepath.Dir(current)==current{break}
 }
 return absolute,nil
}

func urlKey(raw string)string{
 value,err:=url.Parse(raw)
 if err!=nil{return strings.ToLower(raw)}
 value.Scheme=strings.ToLower(value.Scheme)
 value.Host=strings.ToLower(value.Host)
 value.Path=strings.TrimRight(value.Path,"/")
 if value.Path==""{value.Path="/"}
 value.Fragment=""
 return value.String()
}

func check(root string)[]issue{
 raw,err:=os.ReadFile(filepath.Join(root,"REFERENCES.md"))
 if errors.Is(err,os.ErrNotExist){
  return []issue{{Code:"MISSING_REFERENCES",Message:"Repository root must contain REFERENCES.md."}}
 }
 if err!=nil||!utf8.Valid(raw){
  return []issue{{Code:"ENCODING",Message:"REFERENCES.md must be UTF-8 text."}}
 }
 first:="";firstLine:=0;found:=[]issue{}
 count:=0;empty:=false;seen:=map[string]int{}
 for i,line:=range strings.Split(string(raw),"\n"){
  n:=i+1
  if first==""&&strings.TrimSpace(line)!=""{first=strings.TrimSpace(line);firstLine=n}
  if strings.TrimSpace(line)=="No external references are currently required."{empty=true}
  if local.MatchString(line){found=append(found,issue{"LOCAL_PATH",n,"Do not cite local, sandbox or file URLs."})}
  if ephemeral.MatchString(line){found=append(found,issue{"EPHEMERAL_AI",n,"Do not cite assistant chats or search result pages."})}
  parts:=entry.FindStringSubmatch(line)
  if parts==nil{continue}
  count++
  if !strings.Contains(parts[3],"Used for:"){
   found=append(found,issue{"MISSING_ROLE",n,"Reference must contain Used for:."})
  }
  key:=urlKey(parts[2])
  if previous,ok:=seen[key];ok{
   found=append(found,issue{"DUPLICATE",n,fmt.Sprintf("Duplicate source URL; first entry is on line %d.",previous)})
  }else{seen[key]=n}
 }
 if first!="# References"{found=append(found,issue{"HEADING",firstLine,"First nonempty line must be # References."})}
 if empty&&count>0{found=append(found,issue{"EMPTY_WITH_ENTRIES",0,"Remove the empty-state sentence when entries exist."})}
 if !empty&&count==0{found=append(found,issue{"NO_ENTRIES",0,"Add an entry or the exact empty-state sentence."})}
 return found
}

func run(args []string)int{
 flags:=flag.NewFlagSet("references_page_check",flag.ContinueOnError)
 flags.SetOutput(os.Stderr)
 init:=flags.Bool("init",false,"create the canonical reference page if missing")
 asJSON:=flags.Bool("json",false,"emit JSON findings")
 if err:=flags.Parse(args);err!=nil{return 2}
 if flags.NArg()>1{fmt.Fprintln(os.Stderr,"expected at most one path");return 2}
 path:="."
 if flags.NArg()==1{path=flags.Arg(0)}
 root,err:=repoRoot(path)
 if err!=nil{fmt.Fprintln(os.Stderr,err);return 2}
 target:=filepath.Join(root,"REFERENCES.md")
 if *init {
  if _,err:=os.Stat(target);errors.Is(err,os.ErrNotExist){
   if err:=os.WriteFile(target,[]byte("# References\n\nNo external references are currently required.\n"),0644);err!=nil{
    fmt.Fprintln(os.Stderr,err);return 2
   }
  }else if err!=nil{fmt.Fprintln(os.Stderr,err);return 2}
 }
 findings:=check(root)
 if *asJSON{
  encoder:=json.NewEncoder(os.Stdout);encoder.SetIndent("","  ")
  if err:=encoder.Encode(findings);err!=nil{fmt.Fprintln(os.Stderr,err);return 2}
 }else{
  for _,f:=range findings{
   location:=""
   if f.Line>0{location=fmt.Sprintf(" line %d",f.Line)}
   fmt.Printf("ERROR %s%s: %s\n",f.Code,location,f.Message)
  }
  if len(findings)==0{fmt.Println("Repository references check: PASS")}else{
   fmt.Printf("Repository references check: %d issue(s)\n",len(findings))
  }
 }
 if len(findings)>0{return 1}
 return 0
}

func main(){os.Exit(run(os.Args[1:]))}
