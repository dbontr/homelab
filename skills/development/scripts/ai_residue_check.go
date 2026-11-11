// Detect assistant-specific development files and attribution in a working tree.
package main

import (
 "bytes"
 "flag"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "sort"
 "strings"
 "unicode/utf8"
)

type globs []string
func(g *globs)String()string{return strings.Join(*g,",")}
func(g *globs)Set(value string)error{*g=append(*g,value);return nil}

type finding struct{Code,Path,Detail string}

var skip=map[string]bool{".git":true,"node_modules":true,".venv":true,"venv":true,"__pycache__":true,".pytest_cache":true}
var forbiddenFiles=map[string]bool{"agents.md":true,"claude.md":true,"gemini.md":true,".cursorrules":true,".clinerules":true}
var forbiddenDirs=map[string]bool{".agents":true,".claude":true,".cursor":true,".continue":true,".codex":true,".roo":true,".cline":true,".windsurf":true}
var patterns=[]struct{code string;match *regexp.Regexp}{
 {"AI_ATTRIBUTION",regexp.MustCompile(`(?i)\b(generated|written|created)\s+(by|with)\s+(an?\s+)?(ai|chatgpt|claude|copilot|gemini|codex)\b`)},
 {"AI_ASSISTED",regexp.MustCompile(`(?i)\bai[- ]assisted\b`)},
 {"AI_COAUTHOR",regexp.MustCompile(`(?im)^\s*co-authored-by:\s*.*\b(chatgpt|openai|claude|anthropic|copilot|gemini|codex)\b`)},
 {"ASSISTANT_TRANSCRIPT",regexp.MustCompile(`(?i)\b(chatgpt|claude|copilot|gemini|codex)\s+(prompt|transcript|conversation)\b`)},
}

func candidatePaths(root string)([]string,error){
 cmd:=exec.Command("git","-C",root,"ls-files","--cached","--others","--exclude-standard","-z")
 if data,err:=cmd.Output();err==nil{
  result:=[]string{}
  for _,p:=range bytes.Split(data,[]byte{0}){if len(p)>0{result=append(result,string(p))}}
  return result,nil
 }
 var result []string
 err:=filepath.WalkDir(root,func(path string,entry os.DirEntry,walkErr error)error{
  if walkErr!=nil{return walkErr}
  if entry.IsDir(){
   if path!=root&&skip[entry.Name()]{return filepath.SkipDir}
   return nil
  }
  if !entry.Type().IsRegular(){return nil}
  rel,err:=filepath.Rel(root,path)
  if err!=nil{return err}
  result=append(result,filepath.ToSlash(rel))
  return nil
 })
 return result,err
}

func pathFinding(relative string)*finding{
 normalized:=strings.ToLower(strings.ReplaceAll(relative,"\\","/"))
 parts:=strings.Split(normalized,"/")
 basename:=parts[len(parts)-1]
 if forbiddenFiles[basename]||strings.HasPrefix(basename,".aider")||
  normalized==".github/copilot-instructions.md"||
  strings.HasPrefix(normalized,".github/instructions/")&&strings.HasSuffix(normalized,".instructions.md")||
  strings.HasPrefix(normalized,".github/prompts/")&&strings.HasSuffix(normalized,".prompt.md"){
  return &finding{"AI_FILE",relative,"Assistant-specific configuration file is not permitted."}
 }
 for _,part:=range parts{
  if forbiddenDirs[part]{return &finding{"AI_PATH",relative,"Assistant-specific directory is not permitted."}}
 }
 return nil
}

func analyzeFile(path,relative string)[]finding{
 stat,err:=os.Lstat(path)
 if err!=nil{return []finding{{"READ_ERROR",relative,err.Error()}}}
 if !stat.Mode().IsRegular()||stat.Size()>2_000_000{return nil}
 data,err:=os.ReadFile(path)
 if err!=nil{return []finding{{"READ_ERROR",relative,err.Error()}}}
 first:=data
 if len(first)>8192{first=first[:8192]}
 if bytes.IndexByte(first,0)>=0||!utf8.Valid(data){return nil}
 result:=[]finding{}
 for _,pattern:=range patterns{
  if match:=pattern.match.Find(data);len(match)>0{
   detail:=strings.Join(strings.Fields(string(match))," ")
   if len(detail)>120{detail=detail[:120]}
   result=append(result,finding{pattern.code,relative,detail})
  }
 }
 return result
}

func check(root string,allowed []string)([]finding,error){
 paths,err:=candidatePaths(root)
 if err!=nil{return nil,err}
 result:=[]finding{}
 for _,relative:=range paths{
  relative=filepath.ToSlash(relative)
  if filepath.IsAbs(relative)||relative==".."||strings.HasPrefix(relative,"../"){continue}
  skipFile:=false
  for _,glob:=range allowed{
   if matched,err:=filepath.Match(glob,relative);err==nil&&matched{skipFile=true;break}
  }
  if skipFile{continue}
  if match:=pathFinding(relative);match!=nil{result=append(result,*match);continue}
  result=append(result,analyzeFile(filepath.Join(root,relative),relative)...)
 }
 sort.Slice(result,func(i,j int)bool{
  if result[i].Path==result[j].Path{return result[i].Code<result[j].Code}
  return result[i].Path<result[j].Path
 })
 return result,nil
}

func run(args []string)int{
 flags:=flag.NewFlagSet("ai_residue_check",flag.ContinueOnError)
 var allow globs
 flags.Var(&allow,"allow","allow a repository-relative path glob; repeatable")
 if err:=flags.Parse(args);err!=nil{return 2}
 if flags.NArg()>1{fmt.Fprintln(os.Stderr,"expected at most one repository path");return 2}
 root:="."
 if flags.NArg()==1{root=flags.Arg(0)}
 absolute,err:=filepath.Abs(root)
 if err!=nil{fmt.Fprintln(os.Stderr,err);return 2}
 stat,err:=os.Stat(absolute)
 if err!=nil||!stat.IsDir(){fmt.Fprintln(os.Stderr,"repository root is not a directory");return 2}
 found,err:=check(absolute,allow)
 if err!=nil{fmt.Fprintln(os.Stderr,err);return 2}
 if len(found)==0{fmt.Println("AI-development residue check: PASS");return 0}
 for _,item:=range found{
  fmt.Printf("ERROR %s %s: %s\n",item.Code,item.Path,item.Detail)
 }
 fmt.Printf("AI-development residue check: %d finding(s)\n",len(found))
 return 1
}

func main(){os.Exit(run(os.Args[1:]))}
