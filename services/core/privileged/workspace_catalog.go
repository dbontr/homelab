package privileged

import (
    "bytes"
    "encoding/json"
    "errors"
    "net/url"
    "strings"
)

// External image references are operator-selected immutable OCI digests.
// They are exact strings, not registry prefixes or wildcard permissions.
func validWorkspaceImageReference(reference string) bool {
    name,digest,ok:=strings.Cut(reference,"@sha256:")
    if !ok||len(name)<3||len(name)>190||len(digest)!=64 ||
        strings.ContainsAny(name,"\\ 	\r\n@?#") || strings.HasPrefix(name,"/") ||
        strings.HasSuffix(name,"/") || strings.Contains(name,"//"){
        return false
    }
    for _,c:=range name{
        if !(c>='a'&&c<='z'||c>='0'&&c<='9'||
            c=='.'||c=='/'||c=='_'||c=='-'||c==':'){
            return false
        }
    }
    for _,c:=range digest{
        if !(c>='0'&&c<='9'||c>='a'&&c<='f'){
            return false
        }
    }
    return true
}

type workspaceCatalogAction struct {
    Host string `json:"host"`
    ImageID string `json:"image_id,omitempty"`
    Image string `json:"image,omitempty"`
    Architecture string `json:"architecture,omitempty"`
    Description string `json:"description,omitempty"`
    ID string `json:"id,omitempty"`
    Name string `json:"name,omitempty"`
    Version string `json:"version,omitempty"`
    Platform string `json:"platform,omitempty"`
    Distribution string `json:"distribution,omitempty"`
    SourceURL string `json:"source_url,omitempty"`
    LicenseNotes string `json:"license_notes,omitempty"`
    ReviewNote string `json:"review_note,omitempty"`
    Status string `json:"status,omitempty"`
    PoolKind string `json:"pool_kind,omitempty"`
    PoolID string `json:"pool_id,omitempty"`
    Kind string `json:"kind,omitempty"`
    Reason string `json:"reason,omitempty"`
    ExpectedRevision int `json:"expected_revision,omitempty"`
}

func validCatalogKeys(fields map[string]any, allowed ...string) bool {
    keys:=make(map[string]bool,len(allowed)+1)
    keys["host"]=true
    for _,key:=range allowed{keys[key]=true}
    for key:=range fields{
        if !keys[key]{return false}
    }
    return true
}

func (p *WorkspacePolicy) catalogArgs(operation string, fields map[string]any) (string,map[string]any,error) {
    raw,err:=json.Marshal(fields)
    if err!=nil||len(raw)>16<<10{
        return "",nil,errors.New("Workspace catalog request exceeds limit")
    }
    decoder:=json.NewDecoder(bytes.NewReader(raw))
    decoder.DisallowUnknownFields()
    var input workspaceCatalogAction
    if err:=decoder.Decode(&input);err!=nil||
        !workspaceIdentifier.MatchString(input.Host){
        return "",nil,errors.New("invalid Workspace catalog request")
    }
    id:=func(value string)bool{
        return value!=""&&workspaceIdentifier.MatchString(value)
    }
    switch operation{
    case "infrastructure__workspace_register_image":
        if !validCatalogKeys(fields,"image_id","image","architecture","description") ||
            !id(input.ImageID)||!containsID(p.Images,input.ImageID)||
            !validWorkspaceImageReference(input.Image)||
            !containsID(p.ImageReferences,input.Image)||
            len(input.Description)>512||
            input.Architecture!=""&&input.Architecture!="amd64"&&input.Architecture!="arm64"{
            return "",nil,errors.New("Workspace image is outside the operator catalog")
        }
        return "pool_register_image",map[string]any{
            "image_id":input.ImageID,"image":input.Image,
            "description":input.Description,"architecture":input.Architecture,
        },nil
    case "infrastructure__workspace_propose_program":
        if !validCatalogKeys(fields,"id","name","description","version","platform","distribution","source_url","license_notes")||
            !id(input.ID)||!containsID(p.Programs,input.ID)||
            strings.TrimSpace(input.Name)==""||len(input.Name)>128||
            len(input.Description)>1024||len(input.Version)>64||
            len(input.LicenseNotes)>1024||
            !validProgramMetadata(input.Platform,input.Distribution,input.SourceURL){
            return "",nil,errors.New("Workspace program proposal is outside operator policy")
        }
        return "pool_propose_program",map[string]any{
            "id":input.ID,"name":input.Name,"description":input.Description,
            "version":input.Version,"platform":input.Platform,
            "distribution":input.Distribution,"source_url":input.SourceURL,
            "license_notes":input.LicenseNotes,
        },nil
    case "infrastructure__workspace_review_program":
        if !validCatalogKeys(fields,"id","status","review_note","pool_kind","pool_id","expected_revision")||
            !id(input.ID)||!containsID(p.Programs,input.ID)||
            input.ExpectedRevision<1||input.ExpectedRevision>1_000_000||
            strings.TrimSpace(input.ReviewNote)==""||len(input.ReviewNote)>512||
            !validProgramStatus(input.Status)||
            !validCatalogAssociation(p,input.Status,input.PoolKind,input.PoolID){
            return "",nil,errors.New("Workspace program review is outside operator policy")
        }
        return "pool_review_program",map[string]any{
            "id":input.ID,"status":input.Status,"review_note":input.ReviewNote,
            "pool_kind":input.PoolKind,"pool_id":input.PoolID,
            "expected_revision":input.ExpectedRevision,
        },nil
    case "infrastructure__workspace_update_entry":
        if !validCatalogKeys(fields,"kind","id","status","reason","expected_revision")||
            !id(input.ID)||input.ExpectedRevision<1||input.ExpectedRevision>1_000_000||
            strings.TrimSpace(input.Reason)==""||len(input.Reason)>512||
            input.Status!="active"&&input.Status!="disabled"&&input.Status!="retired"{
            return "",nil,errors.New("Workspace catalog state transition is invalid")
        }
        switch input.Kind{
        case "image":
            if !containsID(p.Images,input.ID){
                return "",nil,errors.New("image is outside operator policy")
            }
        case "software":
            if !containsID(p.Software,input.ID){
                return "",nil,errors.New("software is outside operator policy")
            }
        default:
            return "",nil,errors.New("unknown catalog entry kind")
        }
        return "pool_update_entry",map[string]any{
            "id":input.ID,"kind":input.Kind,"status":input.Status,
            "reason":input.Reason,"expected_revision":input.ExpectedRevision,
        },nil
    default:
        return "",nil,errors.New("unregistered Workspace catalog operation")
    }
}

func validProgramMetadata(platform,distribution,sourceURL string)bool{
    switch platform{
    case "linux-container":
        if distribution!="apt"&&distribution!="oci"{return false}
    case "windows-native","macos-native":
        if distribution!="native"{return false}
    default:
        return false
    }
    u,err:=url.Parse(sourceURL)
    return err==nil&&len(sourceURL)<=512&&u.Scheme=="https"&&
        u.Hostname()!=""&&u.User==nil&&u.Fragment==""&&
        !strings.ContainsAny(sourceURL,"\\\x00\r\n\t ")
}

func validProgramStatus(value string)bool{
    switch value{
    case "reviewed","available","disabled","rejected","retired":
        return true
    }
    return false
}

func validCatalogAssociation(p *WorkspacePolicy,status,kind,id string)bool{
    if kind==""&&id==""{
        return status!="available"
    }
    if status!="available"{
        return false
    }
    switch kind{
    case "image":
        return workspaceIdentifier.MatchString(id)&&containsID(p.Images,id)
    case "software":
        return workspaceIdentifier.MatchString(id)&&containsID(p.Software,id)
    }
    return false
}
