// Rank evaluated idea candidates with a transparent pool-relative heuristic.
package main

import (
 "encoding/json"
 "errors"
 "flag"
 "fmt"
 "io"
 "math"
 "os"
 "path/filepath"
 "sort"
 "strings"
)

type candidate map[string]any

type optionalFloat struct{Value float64;Provided bool}
func(o *optionalFloat)String()string{return fmt.Sprintf("%g",o.Value)}
func(o *optionalFloat)SetValue(value string)error{
 var number float64
 if _,err:=fmt.Sscan(value,&number);err!=nil{return err}
 o.Value=number;o.Provided=true;return nil
}
func(o *optionalFloat)Set(value string)error{return o.SetValue(value)}

type optionalInt struct{Value int;Provided bool}
func(o *optionalInt)String()string{return fmt.Sprintf("%d",o.Value)}
func(o *optionalInt)Set(value string)error{
 var number int
 if _,err:=fmt.Sscan(value,&number);err!=nil{return err}
 o.Value=number;o.Provided=true;return nil
}

type options struct{
 Beta,FeasibilityWeight float64
 Top,MaxPerFamily optionalInt
 MinCoherence,MinTractability optionalFloat
}

var components=[]string{"coherence","distinctiveness","tractability"}

func readCandidates(raw []byte)([]candidate,error){
 decoder:=json.NewDecoder(strings.NewReader(string(raw)))
 decoder.UseNumber()
 var payload map[string]any
 if err:=decoder.Decode(&payload);err!=nil{return nil,err}
 var rest any
 if err:=decoder.Decode(&rest);err!=io.EOF{return nil,errors.New("unexpected JSON after candidate list")}
 list,ok:=payload["candidates"].([]any)
 if !ok||len(list)==0{return nil,errors.New("input must contain a nonempty candidates array")}
 items:=make([]candidate,0,len(list))
 ids:=map[string]bool{}
 for i,item:=range list{
  value,ok:=item.(map[string]any)
  if !ok{return nil,fmt.Errorf("candidates[%d] must be an object",i)}
  id,ok:=value["id"].(string)
  if !ok||strings.TrimSpace(id)==""{return nil,fmt.Errorf("candidates[%d].id must be a nonempty string",i)}
  if ids[id]{return nil,fmt.Errorf("duplicate candidate id: %s",id)}
  ids[id]=true
  title,ok:=value["title"].(string)
  if !ok||strings.TrimSpace(title)==""{return nil,fmt.Errorf("candidates[%d].title must be a nonempty string",i)}
  if rawFamily,exists:=value["family"];exists{
   family,ok:=rawFamily.(string)
   if !ok||strings.TrimSpace(family)==""{return nil,fmt.Errorf("candidates[%d].family must be a nonempty string",i)}
  }
  for _,field:=range components{
   number,valid:=scoreValue(value[field])
   if !valid||number<0||number>5{
    return nil,fmt.Errorf("candidates[%d].%s must be a finite number between 0 and 5",i,field)
   }
  }
  items=append(items,value)
 }
 return items,nil
}

func scoreValue(value any)(float64,bool){
 number,ok:=value.(json.Number)
 if !ok{return 0,false}
 result,err:=number.Float64()
 if err!=nil||math.IsNaN(result)||math.IsInf(result,0){return 0,false}
 return result,true
}

func validateOptions(o options)error{
 for _,v:=range []float64{o.Beta,o.FeasibilityWeight}{
  if math.IsNaN(v)||math.IsInf(v,0)||v<0||v>1{return errors.New("beta and feasibility_weight must be between 0 and 1")}
 }
 for _,pair:=range []optionalFloat{o.MinCoherence,o.MinTractability}{
  if pair.Provided&&(math.IsNaN(pair.Value)||math.IsInf(pair.Value,0)||pair.Value<0||pair.Value>5){
   return errors.New("minimum scores must be between 0 and 5")
  }
 }
 if o.Top.Provided&&o.Top.Value<1{return errors.New("top must be positive")}
 if o.MaxPerFamily.Provided&&o.MaxPerFamily.Value<1{return errors.New("max_per_family must be positive")}
 return nil
}

func zs(values []float64)[]float64{
 result:=make([]float64,len(values))
 if len(values)==0{return result}
 mean:=0.0
 for _,v:=range values{mean+=v}
 mean/=float64(len(values))
 variance:=0.0
 for _,v:=range values{variance+=math.Pow(v-mean,2)}
 variance/=float64(len(values))
 if variance<=0{return result}
 standard:=math.Sqrt(variance)
 for i,v:=range values{result[i]=(v-mean)/standard}
 return result
}

func optionValueInt(v optionalInt)any{
 if v.Provided{return v.Value}
 return nil
}
func optionValueFloat(v optionalFloat)any{
 if v.Provided{return v.Value}
 return nil
}

func rank(items []candidate,o options)(map[string]any,error){
 if err:=validateOptions(o);err!=nil{return nil,err}
 eligible:=[]candidate{}
 excluded:=[]map[string]any{}
 for _,item:=range items{
  reasons:=[]string{}
  coherence,_:=scoreValue(item["coherence"])
  tractability,_:=scoreValue(item["tractability"])
  if o.MinCoherence.Provided&&coherence<o.MinCoherence.Value{
   reasons=append(reasons,fmt.Sprintf("coherence below %g",o.MinCoherence.Value))
  }
  if o.MinTractability.Provided&&tractability<o.MinTractability.Value{
   reasons=append(reasons,fmt.Sprintf("tractability below %g",o.MinTractability.Value))
  }
  if len(reasons)>0{excluded=append(excluded,map[string]any{"candidate":item,"reasons":reasons})}else{eligible=append(eligible,item)}
 }
 zValues:=map[string][]float64{}
 for _,field:=range components{
  values:=make([]float64,len(eligible))
  for i,item:=range eligible{values[i],_=scoreValue(item[field])}
  zValues[field]=zs(values)
 }
 ranked:=make([]candidate,0,len(eligible))
 for i,item:=range eligible{
  score:=(1-o.FeasibilityWeight)*((1-o.Beta)*zValues["coherence"][i]+o.Beta*zValues["distinctiveness"][i])+
   o.FeasibilityWeight*zValues["tractability"][i]
  copy:=candidate{}
  for key,value:=range item{copy[key]=value}
  copy["fusion_score"]=score
  copy["z_scores"]=map[string]float64{
   "coherence":zValues["coherence"][i],
   "distinctiveness":zValues["distinctiveness"][i],
   "tractability":zValues["tractability"][i],
  }
  ranked=append(ranked,copy)
 }
 sort.Slice(ranked,func(i,j int)bool{
  a,b:=ranked[i],ranked[j]
  if a["fusion_score"].(float64)!=b["fusion_score"].(float64){return a["fusion_score"].(float64)>b["fusion_score"].(float64)}
  ac,_:=scoreValue(a["coherence"]);bc,_:=scoreValue(b["coherence"])
  if ac!=bc{return ac>bc}
  at,_:=scoreValue(a["tractability"]);bt,_:=scoreValue(b["tractability"])
  if at!=bt{return at>bt}
  return a["id"].(string)<b["id"].(string)
 })
 selected:=[]candidate{}
 counts:=map[string]int{}
 for index,item:=range ranked{
  item["rank"]=index+1
  family,ok:=item["family"].(string)
  if !ok{family="__ungrouped__:"+item["id"].(string)}
  if o.MaxPerFamily.Provided&&counts[family]>=o.MaxPerFamily.Value{continue}
  counts[family]++
  selected=append(selected,item)
  if o.Top.Provided&&len(selected)>=o.Top.Value{break}
 }
 return map[string]any{
  "method":"pool-relative heuristic, not trained Alien inference",
  "parameters":map[string]any{
   "beta":o.Beta,"feasibility_weight":o.FeasibilityWeight,
   "top":optionValueInt(o.Top),"max_per_family":optionValueInt(o.MaxPerFamily),
   "min_coherence":optionValueFloat(o.MinCoherence),
   "min_tractability":optionValueFloat(o.MinTractability),
  },
  "counts":map[string]int{
   "input":len(items),"eligible":len(eligible),"selected":len(selected),"excluded":len(excluded),
  },
  "selected":selected,"ranked":ranked,"excluded":excluded,
  "caveat":"Ratings are subjective assessments; scores are relative to this candidate pool and do not establish novelty or success probabilities.",
 },nil
}

func run(args []string)error{
 input:=""
 if len(args)>0&&!strings.HasPrefix(args[0],"-"){input=args[0];args=args[1:]}
 flags:=flag.NewFlagSet("rank_candidates",flag.ContinueOnError)
 beta:=flags.Float64("beta",0.65,"distinctiveness share of non-tractability score (0-1)")
 feasibility:=flags.Float64("feasibility-weight",0.20,"tractability share (0-1)")
 output:=flags.String("output","","write JSON to this path instead of stdout")
 var top,perFamily optionalInt
 var minC,minT optionalFloat
 flags.Var(&top,"top","number of ideas to select")
 flags.Var(&perFamily,"max-per-family","maximum selected ideas from each family")
 flags.Var(&minC,"min-coherence","minimum coherence (0-5)")
 flags.Var(&minT,"min-tractability","minimum tractability (0-5)")
 if err:=flags.Parse(args);err!=nil{return err}
 if flags.NArg()>1{return errors.New("expected a single input JSON path")}
 if input==""&&flags.NArg()==1{input=flags.Arg(0)}
 if input==""{return errors.New("usage: rank_candidates.go INPUT.json [--top N] [--output FILE]")}
 var raw []byte
 var err error
 if input=="-"{raw,err=io.ReadAll(io.LimitReader(os.Stdin,8<<20))}else{raw,err=os.ReadFile(input)}
 if err!=nil{return err}
 items,err:=readCandidates(raw)
 if err!=nil{return err}
 result,err:=rank(items,options{
  Beta:*beta,FeasibilityWeight:*feasibility,Top:top,MaxPerFamily:perFamily,
  MinCoherence:minC,MinTractability:minT,
 })
 if err!=nil{return err}
 encoded,err:=json.MarshalIndent(result,"","  ")
 if err!=nil{return err}
 encoded=append(encoded,'\n')
 if *output==""{_,err=os.Stdout.Write(encoded);return err}
 if err:=os.MkdirAll(filepath.Dir(*output),0755);err!=nil{return err}
 return os.WriteFile(*output,encoded,0644)
}

func main(){
 if err:=run(os.Args[1:]);err!=nil{
  fmt.Fprintln(os.Stderr,"candidate ranking:",err)
  os.Exit(2)
 }
}
