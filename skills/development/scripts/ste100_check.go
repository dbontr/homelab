// Mechanical ASD-STE100 Issue 9 precheck. It does not verify dictionary use.
package main

import (
 "encoding/json"
 "flag"
 "fmt"
 "io"
 "os"
 "regexp"
 "strings"
 "unicode"
 "unicode/utf8"
)

type issue struct{
 Code string `json:"code"`
 Severity string `json:"severity"`
 Line int `json:"line"`
 Message string `json:"message"`
 Text string `json:"text"`
}

var fenced=regexp.MustCompile(`(?s)\x60{3}.*?\x60{3}|~~~.*?~~~`)
var inline=regexp.MustCompile(`\x60[^\x60\n]+\x60`)
var urls=regexp.MustCompile(`https?://\S+`)
var paren=regexp.MustCompile(`\([^()]*\)`)
var quoted=regexp.MustCompile(`"[^"\n]+"`)
var unit=regexp.MustCompile(`\b\d+(\.\d+)?\s*(%|[A-Za-z°]+)\b`)
var word=regexp.MustCompile(`\b([A-Za-z]+(-[A-Za-z]+)*|[A-Za-z]*\d+[A-Za-z0-9._/-]*)\b`)
var listPrefix=regexp.MustCompile(`^([-*+]\s+|\d+[.)]\s+)`)
var rules=[]struct{code,severity,message string;pattern *regexp.Regexp}{
 {"SEMICOLON","error","Semicolons are not permitted in STE.",regexp.MustCompile(`;`)},
 {"CONTRACTION","error","Do not shorten technical text with contractions.",regexp.MustCompile(`(?i)\b(can't|won't|isn't|aren't|wasn't|weren't|don't|doesn't|didn't|hasn't|haven't|hadn't|shouldn't|wouldn't|couldn't|mustn't|it's|that's|there's|we're|you're|they're|I've|we've|you've|they've|I'll|we'll|you'll|they'll)\b`)},
 {"COMPLEX_VERB","warning","Review this auxiliary verb form against Issue 9.",regexp.MustCompile(`(?i)\b(has|have|had)\s+\w+(ed|en)\b|\b(is|are|was|were|be|been|being)\s+\w+ing\b`)},
 {"PASSIVE_VOICE","warning","Review possible passive voice.",regexp.MustCompile(`(?i)\b(is|are|was|were|be|been|being|will be|can be|must be|may be|should be)\s+\w+(ed|en)\b`)},
 {"PHRASAL_VERB","warning","Prefer an approved single verb when appropriate.",regexp.MustCompile(`(?i)\b(spin up|reach out|kick off|take down|set up|shut down|log in|log out|look up|check out|carry out|back up|roll back|clean up|tear down)\b`)},
 {"SPELLING","warning","Use American English spelling.",regexp.MustCompile(`(?i)\b(colour|colours|favour|favours|centre|centres|behaviour|behaviours|organisation|organisations|optimise|optimised|optimising|analyse|analysed|analysing)\b`)},
}

func sentences(line string)[]string{
 result:=[]string{}
 start:=0
 for i,r:=range line{
  if r!='.'&&r!='!'&&r!='?'{continue}
  end:=i+utf8.RuneLen(r)
  if end>=len(line){continue}
  rest:=line[end:]
  skip:=0
  for skip<len(rest) {
   next,size:=utf8.DecodeRuneInString(rest[skip:])
   if !unicode.IsSpace(next){break}
   skip+=size
  }
  if skip==0||skip>=len(rest){continue}
  next,_:=utf8.DecodeRuneInString(rest[skip:])
  if !unicode.IsUpper(next)&&!unicode.IsDigit(next)&&!strings.ContainsRune("\"'([",next){continue}
  result=append(result,strings.TrimSpace(line[start:end]))
  start=end+skip
 }
 if end:=strings.TrimSpace(line[start:]);end!=""{result=append(result,end)}
 return result
}

func steWords(sentence string)int{
 working:=paren.ReplaceAllString(sentence," GROUP ")
 working=quoted.ReplaceAllString(working," QUOTE ")
 working=unit.ReplaceAllString(working," VALUE ")
 return len(word.FindAllString(working,-1))
}

func scrub(raw string)string{
 value:=fenced.ReplaceAllString(raw," ")
 value=inline.ReplaceAllString(value," LITERAL ")
 return urls.ReplaceAllString(value," URL ")
}

func check(raw,mode string)[]issue{
 cleaned:=scrub(raw)
 limit:=25
 if mode=="procedure"||mode=="safety"{limit=20}
 result:=[]issue{}
 for i,line:=range strings.Split(cleaned,"\n"){
  text:=strings.TrimSpace(line)
  if text==""||strings.HasPrefix(text,"#"){continue}
  text=listPrefix.ReplaceAllString(text,"")
  for _,sentence:=range sentences(text){
   if count:=steWords(sentence);count>limit{
    result=append(result,issue{"SENTENCE_LENGTH","error",i+1,
     fmt.Sprintf("Sentence has %d words; %s limit is %d.",count,mode,limit),sentence})
   }
   for _,rule:=range rules{
    if rule.pattern.MatchString(sentence){
     result=append(result,issue{rule.code,rule.severity,i+1,rule.message,sentence})
    }
   }
  }
 }
 if mode=="description"{
  position:=1
  for _,block:=range regexp.MustCompile(`\n\s*\n`).Split(cleaned,-1){
   lines:=strings.Split(block,"\n")
   summary:=[]string{}
   isList:=false
   for _,line:=range lines{
    line=strings.TrimSpace(line)
    if line==""{continue}
    if listPrefix.MatchString(line){isList=true}
    summary=append(summary,line)
   }
   if len(summary)>0&&!strings.HasPrefix(summary[0],"#")&&!isList{
    count:=0
    for _,line:=range summary{count+=len(sentences(line))}
    if count>6{
     result=append(result,issue{"PARAGRAPH_LENGTH","error",position,
      fmt.Sprintf("Descriptive paragraph has %d sentences; maximum is 6.",count),
      strings.Join(summary," ")})
    }
   }
   position+=strings.Count(block,"\n")+2
  }
 }
 return result
}

func run(args []string)int{
 flags:=flag.NewFlagSet("ste100_check",flag.ContinueOnError)
 mode:=flags.String("mode","description","procedure, description or safety")
 asJSON:=flags.Bool("json",false,"emit JSON findings")
 if err:=flags.Parse(args);err!=nil{return 2}
 if *mode!="procedure"&&*mode!="description"&&*mode!="safety"{
  fmt.Fprintln(os.Stderr,"invalid mode: expected procedure, description or safety")
  return 2
 }
 if flags.NArg()>1{fmt.Fprintln(os.Stderr,"expected at most one file path");return 2}
 var data []byte
 var err error
 if flags.NArg()==1 {data,err=os.ReadFile(flags.Arg(0))}else{data,err=io.ReadAll(io.LimitReader(os.Stdin,4<<20))}
 if err!=nil||!utf8.Valid(data){fmt.Fprintln(os.Stderr,"invalid UTF-8 input");return 2}
 found:=check(string(data),*mode)
 if *asJSON{
  encoder:=json.NewEncoder(os.Stdout);encoder.SetIndent("","  ")
  if err:=encoder.Encode(found);err!=nil{fmt.Fprintln(os.Stderr,err);return 2}
 }else if len(found)==0{
  fmt.Println("ASD-STE100 mechanical precheck: PASS")
 }else{
  for _,item:=range found{
   fmt.Printf("%s %s line %d: %s\n  %s\n",
    strings.ToUpper(item.Severity),item.Code,item.Line,item.Message,item.Text)
  }
  fmt.Printf("ASD-STE100 mechanical precheck: %d issue(s)\n",len(found))
 }
 if len(found)!=0{return 1}
 return 0
}

func main(){os.Exit(run(os.Args[1:]))}
