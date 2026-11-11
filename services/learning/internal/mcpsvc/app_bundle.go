package mcpsvc

import (
    "bytes"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "io/fs"
    "mime"
    "net/http"
    "net/url"
    "os"
    "path"
    "path/filepath"
    "strings"
    "time"

    "github.com/modelcontextprotocol/go-sdk/mcp"
)

// LearningAppBundle owns an immutable verified snapshot of public Rust/WASM
// assets. Learning records, model credentials and MCP tokens are never assets.
type LearningAppBundle struct {
    origin string
    html string
    assets map[string][]byte
}

func learningAssetName(name string) bool {
    if name==""||len(name)>240||strings.HasPrefix(name,"/")||
        path.Clean(name)!=name||strings.ContainsAny(name,"\\\x00\r\n"){
        return false
    }
    for _,part:=range strings.Split(name,"/"){
        if part==""||part=="."||part==".."||strings.HasPrefix(part,"."){return false}
    }
    return true
}

func learningAssetOrigin(origin string)error{
    u,err:=url.Parse(origin)
    if err!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||
        u.Opaque!=""||u.Path!=""||u.RawQuery!=""||u.Fragment!=""||
        u.String()!=origin||strings.ContainsAny(origin,"\"'<> \t\r\n"){
        return errors.New("Learning assets require an exact HTTPS origin")
    }
    return nil
}

// LoadLearningAppBundle verifies the complete shared app distribution before
// serving any resource. Any extra, missing or changed file invalidates it.
func LoadLearningAppBundle(directory,origin string)(*LearningAppBundle,error){
    if err:=learningAssetOrigin(origin);err!=nil{return nil,err}
    if !filepath.IsAbs(directory)||filepath.Clean(directory)!=directory{
        return nil,errors.New("Learning asset directory must be canonical")
    }
    resolved,err:=filepath.EvalSymlinks(directory)
    if err!=nil||resolved!=directory{
        return nil,errors.New("Learning asset directory cannot be a symlink")
    }
    meta,err:=os.Lstat(directory)
    if err!=nil||!meta.IsDir()||meta.Mode().Perm()&0022!=0{
        return nil,errors.New("Learning asset directory cannot be writable by others")
    }
    manifestPath:=filepath.Join(directory,"SHA256SUMS")
    info,err:=os.Lstat(manifestPath)
    if err!=nil||!info.Mode().IsRegular()||info.Size()<64||info.Size()>256<<10{
        return nil,errors.New("Learning asset manifest is missing or invalid")
    }
    manifest,err:=os.ReadFile(manifestPath)
    if err!=nil{return nil,err}
    expected:=make(map[string]string)
    for _,line:=range strings.Split(strings.TrimSuffix(string(manifest),"\n"),"\n"){
        fields:=strings.SplitN(line,"  ./",2)
        if len(fields)!=2||len(fields[0])!=64||
            !learningAssetName(fields[1])||expected[fields[1]]!=""{
            return nil,errors.New("invalid Learning asset manifest")
        }
        raw,err:=hex.DecodeString(fields[0])
        if err!=nil||hex.EncodeToString(raw)!=fields[0]{
            return nil,errors.New("invalid Learning asset checksum")
        }
        expected[fields[1]]=fields[0]
    }
    if len(expected)<8||len(expected)>400{
        return nil,errors.New("Learning bundle has unexpected file count")
    }
    public:=map[string][]byte{}
    seen:=map[string]bool{}
    var html string
    var total int64
    err=fs.WalkDir(os.DirFS(directory),".",func(name string,entry fs.DirEntry,walkErr error)error{
        if walkErr!=nil{return walkErr}
        if name=="."{return nil}
        if !learningAssetName(name)||entry.Type()&os.ModeSymlink!=0{
            return errors.New("unsafe Learning asset path")
        }
        if entry.IsDir(){return nil}
        stat,err:=entry.Info()
        if err!=nil||!stat.Mode().IsRegular()||stat.Size()<1||stat.Size()>64<<20{
            return errors.New("invalid Learning asset file")
        }
        if name=="SHA256SUMS"{return nil}
        digest,found:=expected[name]
        if !found{return errors.New("undeclared Learning asset")}
        total+=stat.Size()
        if total>96<<20{return errors.New("Learning bundle exceeds release limit")}
        data,err:=os.ReadFile(filepath.Join(directory,filepath.FromSlash(name)))
        if err!=nil||int64(len(data))!=stat.Size(){
            return errors.New("Learning asset changed during inspection")
        }
        computed:=sha256.Sum256(data)
        if hex.EncodeToString(computed[:])!=digest{
            return errors.New("Learning asset checksum mismatch")
        }
        seen[name]=true
        switch {
        case name=="index.html":
            return nil
        case name=="learning.html":
            marker:=`<meta name="homelab-mcp-view" content="learning">`
            if len(data)>1<<20||!bytes.Contains(data,[]byte(marker))||
                !bytes.Contains(data,[]byte("<head>")){
                return errors.New("Learning App entrypoint marker is missing")
            }
            html=string(data)
            return nil
        case strings.HasPrefix(name,"health-")&&strings.HasSuffix(name,".html"):
            // Shared release pages remain signed by the same manifest, but
            // are not published through the Learning MCP resource.
            return nil
        }
        ext:=strings.ToLower(filepath.Ext(name))
        switch ext {
        case ".js",".mjs",".wasm",".css",".svg",".png",".jpg",".jpeg",
            ".webp",".ico",".woff",".woff2":
            public[name]=data
        default:
            return errors.New("Learning bundle contains an unapproved asset type")
        }
        return nil
    })
    if err!=nil{return nil,err}
    if len(seen)!=len(expected)||html==""||len(public)==0||!seen["index.html"]{
        return nil,errors.New("Learning bundle is incomplete")
    }
    return &LearningAppBundle{origin:origin,html:html,assets:public},nil
}

func (b *LearningAppBundle) resourceHTML()string{
    if b==nil{return ""}
    root:=b.origin+"/mcp-assets/"
    html:=strings.NewReplacer(`src="/`,`src="`+root,`href="/`,`href="`+root).Replace(b.html)
    return strings.Replace(html,"<head>",`<head><base href="`+root+`">`,1)
}

func (b *LearningAppBundle) resourceMeta()mcp.Meta{
    return mcp.Meta{"ui":map[string]any{"csp":map[string]any{
        "resourceDomains":[]string{b.origin},
        "connectDomains":[]string{b.origin},
        "baseUriDomains":[]string{b.origin},
    }}}
}

func (b *LearningAppBundle) ServeStatic(w http.ResponseWriter,r *http.Request){
    if b==nil||r.URL==nil||"https://"+r.Host!=b.origin{
        http.Error(w,"unapproved public asset origin",http.StatusForbidden);return
    }
    if r.Method!=http.MethodGet&&r.Method!=http.MethodHead{
        w.Header().Set("Allow","GET, HEAD")
        http.Error(w,"method not allowed",http.StatusMethodNotAllowed);return
    }
    name:=strings.TrimPrefix(r.URL.Path,"/mcp-assets/")
    if !learningAssetName(name){http.NotFound(w,r);return}
    data,ok:=b.assets[name]
    if !ok{http.NotFound(w,r);return}
    kind:=mime.TypeByExtension(path.Ext(name))
    if strings.HasSuffix(name,".wasm"){kind="application/wasm"}
    if kind==""{kind="application/octet-stream"}
    w.Header().Set("Content-Type",kind)
    w.Header().Set("X-Content-Type-Options","nosniff")
    w.Header().Set("Cross-Origin-Resource-Policy","cross-origin")
    w.Header().Set("Access-Control-Allow-Origin","*")
    w.Header().Set("Cache-Control","public, max-age=3600")
    http.ServeContent(w,r,path.Base(name),time.Time{},bytes.NewReader(data))
}

func (svc *Service) SetLearningAppBundle(bundle *LearningAppBundle)error{
    if svc==nil||bundle==nil||bundle.html==""||bundle.origin==""||
        svc.bundle!=nil{
        return errors.New("Learning App release must be a single verified immutable bundle")
    }
    svc.bundle=bundle
    return nil
}
