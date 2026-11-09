package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

const maxResearchLearningTransfer = 1 << 20

// ResearchLearningRequest identifies a source-verified, consented transfer.
type ResearchLearningRequest struct {
	ProjectID string   `json:"project_id"`
	Title string       `json:"title"`
	Objective string   `json:"objective,omitempty"`
	PassageIDs []string `json:"passage_ids"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	Confirm bool       `json:"confirm,omitempty"`
}

func validateResearchLearningRequest(in ResearchLearningRequest) error {
	if !consoleProjectID.MatchString(in.ProjectID) ||
		strings.TrimSpace(in.Title)=="" || len(in.Title)>160 ||
		len(in.Objective)>2048 || len(in.PassageIDs)==0 || len(in.PassageIDs)>24 {
		return errors.New("invalid research-to-learning scope")
	}
	seen:=map[string]bool{}
	for _,id:=range in.PassageIDs {
		if len(id)>128 || strings.ContainsAny(id,"\x00\r\n /\\") || seen[id] {
			return errors.New("invalid or repeated passage identity")
		}
		seen[id]=true
	}
	return nil
}

// backendObject reads one advertised, bounded JSON tool result.
func (s *Server) backendObject(ctx context.Context, name string, input any, limit int)(map[string]any,error) {
	body,err:=json.Marshal(input)
	if err!=nil{return nil,err}
	reply:=s.invoke(ctx,name,body,json.RawMessage("1"))
	if failed,_:=reply["isError"].(bool);failed{return nil,errors.New("authorized backend operation failed")}
	parts,ok:=reply["content"].([]any)
	if !ok||len(parts)!=1{return nil,errors.New("backend result is not a single text item")}
	part,ok:=parts[0].(map[string]any)
	if !ok{return nil,errors.New("backend returned invalid content")}
	text,ok:=part["text"].(string)
	if !ok||len(text)>limit{return nil,errors.New("backend result exceeds permitted size")}
	var output map[string]any
	if json.Unmarshal([]byte(text),&output)!=nil||output==nil{
		return nil,errors.New("backend returned invalid JSON")
	}
	return output,nil
}

// researchLearningEvidence checks source ownership and exact bytes every time.
// Import repeats this check; its approved pack digest must still match.
func (s *Server) researchLearningEvidence(ctx context.Context, in ResearchLearningRequest)(map[string]any,string,error){
	if err:=validateResearchLearningRequest(in);err!=nil{return nil,"",err}
	if !s.hasBackend("workspace"){return nil,"",errors.New("project research is unavailable")}
	pack,err:=s.backendObject(ctx,"workspace__study_pack_prepare",map[string]any{
		"project_id":in.ProjectID,"title":in.Title,"objective":in.Objective,
		"passage_ids":in.PassageIDs,
	},maxResearchLearningTransfer)
	if err!=nil{return nil,"",err}
	if pack["project"]!=in.ProjectID||pack["title"]!=in.Title||
		pack["source_verified"]!=true||pack["stored"]!=false{
		return nil,"",errors.New("research pack identity or source verification mismatch")
	}
	digest,ok:=pack["sha256"].(string)
	if !ok||len(digest)!=64{return nil,"",errors.New("research pack digest is unavailable")}
	delete(pack,"sha256")
	canonical,err:=json.Marshal(pack)
	if err!=nil{return nil,"",err}
	sum:=sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:])!=digest{return nil,"",errors.New("research pack checksum mismatch")}
	passages,ok:=pack["passages"].([]any)
	if !ok||len(passages)!=len(in.PassageIDs){return nil,"",errors.New("study pack has missing passages")}
	citations:=make([]map[string]string,0,len(passages))
	var document strings.Builder
	fmt.Fprintf(&document,"Research evidence\nProject: %s\nTitle: %s\nObjective: %s\nPack SHA-256: %s\n\n",
		in.ProjectID,in.Title,in.Objective,digest)
	for i,value:=range passages{
		passage,ok:=value.(map[string]any)
		if !ok{return nil,"",errors.New("invalid study passage record")}
		citation,_:=passage["citation_ref"].(string)
		body,_:=passage["body"].(string)
		bodySHA,_:=passage["body_sha256"].(string)
		sourceSHA,_:=passage["original_sha256"].(string)
		origin,_:=passage["original"].(string)
		if citation==""||len(citation)>256||body==""||!utf8.ValidString(body)||
			len(body)>64<<10||len(bodySHA)!=64||len(sourceSHA)!=64{
			return nil,"",errors.New("study passage provenance is incomplete")
		}
		hash:=sha256.Sum256([]byte(body))
		if hex.EncodeToString(hash[:])!=bodySHA{
			return nil,"",errors.New("study passage body hash mismatch")
		}
		citations=append(citations,map[string]string{
			"passage_id":in.PassageIDs[i],"citation_ref":citation,"body_sha256":bodySHA,
		})
		fmt.Fprintf(&document,"Passage %d\nCitation: %s\nOriginal: %s\nOriginal SHA-256: %s\nPassage SHA-256: %s\n\n%s\n\n",
			i+1,citation,origin,sourceSHA,bodySHA,body)
		if document.Len()>maxResearchLearningTransfer{
			return nil,"",errors.New("research evidence exceeds learning import limit")
		}
	}
	preview:=map[string]any{
		"project_id":in.ProjectID,"title":in.Title,"pack_sha256":digest,
		"passages":len(passages),"transfer_bytes":document.Len(),
		"citations":citations,"stored":false,
	}
	return preview,document.String(),nil
}

func (s *Server) researchLearningTool(ctx context.Context,name string,raw json.RawMessage)(any,error){
	var input ResearchLearningRequest
	if err:=json.Unmarshal(raw,&input);err!=nil{return nil,err}
	preview,text,err:=s.researchLearningEvidence(ctx,input)
	if err!=nil{return nil,err}
	if name=="research_learning_preview"{return preview,nil}
	if name!="research_learning_import"{return nil,errors.New("unknown research learning operation")}
	if !input.Confirm||input.ExpectedSHA256!=preview["pack_sha256"]{
		return nil,errors.New("import requires confirmation of the current research pack digest")
	}
	if !s.hasBackend("learning"){return nil,errors.New("learning service unavailable")}
	record,err:=s.backendObject(ctx,"learning__ingest_source_text",map[string]any{
		"label":input.Title+" ("+input.ProjectID+")","kind":"document",
		"mime_type":"text/plain","text":text,
	},32<<10)
	if err!=nil{return nil,err}
	if record["id"]==nil{return nil,errors.New("learning service did not confirm a source")}
	return map[string]any{
		"imported":true,"learning_source":record,
		"pack_sha256":preview["pack_sha256"],"citations":preview["citations"],
		"mastery_awarded":false,
	},nil
}

func (s *Server) researchLearningHTTP(w http.ResponseWriter,r *http.Request) {
	var input ResearchLearningRequest
	if err:=decode(r,&input);err!=nil{writeError(w,400,err.Error());return}
	name:="research_learning_preview"
	if strings.HasSuffix(r.URL.Path,"/import"){name="research_learning_import"}
	raw,_:=json.Marshal(input)
	result,err:=s.researchLearningTool(r.Context(),name,raw)
	if err!=nil{writeError(w,400,err.Error());return}
	if name=="research_learning_import"{writeJSON(w,201,result)}else{writeJSON(w,200,result)}
}
