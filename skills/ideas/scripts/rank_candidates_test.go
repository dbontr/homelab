package main

import (
 "encoding/json"
 "os"
 "path/filepath"
 "reflect"
 "strings"
 "testing"
)

const example=`{"candidates":[
 {"id":"a","title":"Coherent","family":"one","coherence":5,"distinctiveness":1,"tractability":5},
 {"id":"b","title":"Distinctive","family":"two","coherence":3,"distinctiveness":5,"tractability":2},
 {"id":"c","title":"Balanced","family":"two","coherence":4,"distinctiveness":3,"tractability":4}
]}`

func candidates(t *testing.T)[]candidate{
 t.Helper()
 items,err:=readCandidates([]byte(example))
 if err!=nil{t.Fatal(err)}
 return items
}

func TestValidation(t *testing.T){
 for _,input:=range []string{
  `{"candidates":[]}`,
  `{"candidates":[{"id":"a","title":"one","coherence":5,"distinctiveness":3,"tractability":4},{"id":"a","title":"two","coherence":2,"distinctiveness":3,"tractability":3}]}`,
  `{"candidates":[{"id":"x","title":"Bad","coherence":true,"distinctiveness":2,"tractability":2}]}`,
  `{"candidates":[{"id":"x","title":"Bad","family":"","coherence":2,"distinctiveness":2,"tractability":2}]}`,
  `{"candidates":[{"id":"x","title":"Bad","coherence":6,"distinctiveness":2,"tractability":2}]}`,
 }{
  if _,err:=readCandidates([]byte(input));err==nil{t.Fatal("invalid candidate accepted",input)}
 }
 if _,err:=readCandidates([]byte(example));err!=nil{t.Fatal(err)}
}

func TestRankingTradeoffAndDiversity(t *testing.T){
 items:=candidates(t)
 coherent,err:=rank(items,options{Beta:0,FeasibilityWeight:0})
 if err!=nil{t.Fatal(err)}
 distinctive,err:=rank(items,options{Beta:1,FeasibilityWeight:0})
 if err!=nil{t.Fatal(err)}
 if coherent["ranked"].([]candidate)[0]["id"]!="a"||distinctive["ranked"].([]candidate)[0]["id"]!="b"{
  t.Fatal("ranking weights ignored")
 }
 diverse,err:=rank(items,options{Beta:.65,FeasibilityWeight:.20,MaxPerFamily:optionalInt{Value:1,Provided:true}})
 if err!=nil{t.Fatal(err)}
 if len(diverse["selected"].([]candidate))!=2{t.Fatal("family limit ignored")}
 filtered,err:=rank(items,options{Beta:.65,FeasibilityWeight:.20,MinTractability:optionalFloat{Value:4,Provided:true}})
 if err!=nil{t.Fatal(err)}
 if filtered["counts"].(map[string]int)["excluded"]!=1{t.Fatal("filter did not exclude weak ideas")}
}

func TestConstantColumnAndDeterministicTie(t *testing.T){
 if !reflect.DeepEqual(zs([]float64{2,2,2}),[]float64{0,0,0}){
  t.Fatal("constant column generated nonzero score")
 }
 raw:=`{"candidates":[{"id":"z","title":"Z","coherence":3,"distinctiveness":3,"tractability":3},{"id":"a","title":"A","coherence":3,"distinctiveness":3,"tractability":3}]}`
 items,err:=readCandidates([]byte(raw))
 if err!=nil{t.Fatal(err)}
 ranked,err:=rank(items,options{Beta:.65,FeasibilityWeight:.2})
 if err!=nil{t.Fatal(err)}
 if ranked["ranked"].([]candidate)[0]["id"]!="a"{t.Fatal("equal-score ordering was not deterministic")}
 if ranked["ranked"].([]candidate)[0]["fusion_score"].(float64)!=0{
  t.Fatal("constant pool must have finite zero score")
 }
}

func TestCommandLineWritesCanonicalJSON(t *testing.T){
 dir:=t.TempDir()
 input:=filepath.Join(dir,"candidates.json")
 output:=filepath.Join(dir,"out","ranking.json")
 if err:=os.WriteFile(input,[]byte(example),0600);err!=nil{t.Fatal(err)}
 if err:=run([]string{input,"--top","2","--output",output});err!=nil{t.Fatal(err)}
 raw,err:=os.ReadFile(output)
 if err!=nil{t.Fatal(err)}
 if !json.Valid(raw){t.Fatal("ranker output invalid JSON")}
 if strings.Contains(string(raw),"NaN")||strings.Contains(string(raw),"Infinity"){
  t.Fatal("ranker produced nonfinite JSON")
 }
 var result map[string]any
 if err:=json.Unmarshal(raw,&result);err!=nil{t.Fatal(err)}
 if len(result["selected"].([]any))!=2{t.Fatal("top bound ignored")}
}

func TestOptionsRejectInvalidBounds(t *testing.T){
 for _,o:=range []options{
  {Beta:-.1,FeasibilityWeight:.2},
  {Beta:.65,FeasibilityWeight:2},
  {Beta:.65,FeasibilityWeight:.2,Top:optionalInt{Value:0,Provided:true}},
  {Beta:.65,FeasibilityWeight:.2,MinCoherence:optionalFloat{Value:7,Provided:true}},
 }{
  if _,err:=rank(candidates(t),o);err==nil{t.Fatal("invalid ranking parameters accepted",o)}
 }
}
