package agentflow

import (
    "errors"
    "go/format"
    "fmt"
    "strings"
)

// formatGoSource applies the Go parser and formatter to complete source text.
// The caller must enforce file allowlists and expected source digests.
func formatGoSource(source string) (string, error) {
    if len(source) == 0 || len(source) > 24000 {
        return "", errors.New("source is outside formatting limits")
    }
    formatted, err := format.Source([]byte(source))
    if err != nil {
        return "", errors.New("Go source has syntax errors")
    }
    if len(formatted) == 0 || len(formatted) > 24000 {
        return "", errors.New("formatted source exceeds file limit")
    }
    return string(formatted), nil
}

func previewSourceChange(before, after string) string {
 if before==after{return "Source is unchanged"}
 oldLines,newLines:=strings.Split(before,"\n"),strings.Split(after,"\n")
 prefix:=0
 for prefix<len(oldLines)&&prefix<len(newLines)&&oldLines[prefix]==newLines[prefix]{prefix++}
 suffix:=0
 for suffix<len(oldLines)-prefix&&suffix<len(newLines)-prefix&&oldLines[len(oldLines)-1-suffix]==newLines[len(newLines)-1-suffix]{suffix++}
 oldChanged:=oldLines[prefix:len(oldLines)-suffix]
 newChanged:=newLines[prefix:len(newLines)-suffix]
 var out strings.Builder
 fmt.Fprintf(&out,"@@ -%d,%d +%d,%d @@\n",prefix+1,len(oldChanged),prefix+1,len(newChanged))
 for _,line:=range oldChanged {
  if out.Len()>3500{break}
  if len(line)>300{line=line[:300]+"…"}
  out.WriteString("-"+line+"\n")
 }
 for _,line:=range newChanged{
  if out.Len()>7000{break}
  if len(line)>300{line=line[:300]+"…"}
  out.WriteString("+"+line+"\n")
 }
 return out.String()
}
