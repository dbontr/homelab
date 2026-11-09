package agentflow

import (
 "path"
 "strings"
)

// importedSource checks only statically declared references. Dynamic imports,
// generated code and unresolved module roots are deliberately not inferred.
func importedSource(importer CodeOutline, target string) bool {
 if importer.Language!=sourceLanguage(target){return false}
 for _,spec:=range importer.Imports{
  switch importer.Language{
  case "rust":
   module:=strings.TrimSuffix(strings.TrimPrefix(target,"src/"),".rs")
   module=strings.TrimSuffix(module,"/mod")
   module=strings.ReplaceAll(module,"/","::")
   if module==""||module=="lib"||module=="main"{continue}
   var resolved string
   switch{
   case strings.HasPrefix(spec,"crate::"):
    resolved=strings.TrimPrefix(spec,"crate::")
   case strings.HasPrefix(spec,"self::"):
    parent:=strings.TrimSuffix(strings.TrimPrefix(path.Dir(importer.Path),"src"),"/")
    parent=strings.Trim(parent,"/")
    resolved=strings.ReplaceAll(parent,"/","::")
    if resolved!=""{resolved+="::"}
    resolved+=strings.TrimPrefix(spec,"self::")
   case strings.HasPrefix(spec,"super::"):
    parent:=strings.Trim(strings.TrimPrefix(path.Dir(importer.Path),"src"),"/")
    if i:=strings.LastIndex(parent,"/");i>=0{parent=parent[:i]}else{parent=""}
    resolved=strings.ReplaceAll(parent,"/","::")
    if resolved!=""{resolved+="::"}
    resolved+=strings.TrimPrefix(spec,"super::")
   default:continue
   }
   if resolved==module||strings.HasPrefix(resolved,module+"::"){return true}
  case "python":
   module:=strings.TrimSuffix(strings.TrimPrefix(target,"src/"),".py")
   module=strings.TrimSuffix(module,"/__init__")
   module=strings.ReplaceAll(module,"/",".")
   if module!=""&&(spec==module||strings.HasPrefix(spec,module+".")){return true}
  case "c-family":
   if spec==target||spec==path.Base(target)&&path.Dir(importer.Path)==path.Dir(target){return true}
  case "vhdl":
   module:=strings.TrimSuffix(strings.TrimSuffix(path.Base(target),".vhdl"),".vhd")
   if strings.EqualFold(spec,"work."+module+".all")||strings.EqualFold(spec,"work."+module){return true}
  }
 }
 return false
}

func attachGoImpact(report *TestImpact, analysis GoSemanticReport) {
 report.Basis="bounded source/import inference with local Go syntax and type analysis"
 if analysis.Partial||analysis.Truncated||len(analysis.Diagnostics)>0{
  report.Incomplete=true
  report.GoAnalysisPartial=true
 }
 for _,diagnostic:=range analysis.Diagnostics{
  if len(report.GoDiagnostics)>=8{report.Incomplete=true;break}
  if len(diagnostic)>240{diagnostic=diagnostic[:240];report.Incomplete=true}
  report.GoDiagnostics=append(report.GoDiagnostics,diagnostic)
 }
}
