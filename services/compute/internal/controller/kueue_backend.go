package controller

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var errKubernetesNotFound=errors.New("Kubernetes resource not found")
var errKubernetesConflict=errors.New("Kubernetes resource conflict")
var errKueueRecoveryRequired=errors.New("Kubernetes Job execution needs recovery")

type KueueBackend struct{
	endpoint string
	tokenFile string
	client *http.Client
}

func NewKueueBackendFromEnv()(*KueueBackend,error){
	switch os.Getenv("PWM_KUEUE_ENABLED"){
	case "":return nil,nil
	case "1":
	default:return nil,errors.New("PWM_KUEUE_ENABLED must be 1 or unset")
	}
	token,ca:=os.Getenv("PWM_KUEUE_TOKEN_FILE"),os.Getenv("PWM_KUEUE_CA_FILE")
	if token==""||ca==""{return nil,errors.New("projected Kubernetes identity and CA required")}
	info,err:=os.Stat(token)
	if err!=nil||!info.Mode().IsRegular(){return nil,errors.New("Kubernetes identity file unavailable")}
	caPEM,err:=os.ReadFile(ca)
	if err!=nil{return nil,err}
	pool:=x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM){return nil,errors.New("invalid Kubernetes CA")}
	return &KueueBackend{
		endpoint:"https://kubernetes.default.svc",
		tokenFile:token,
		client:&http.Client{Timeout:12*time.Second,Transport:&http.Transport{
			TLSClientConfig:&tls.Config{RootCAs:pool,MinVersion:tls.VersionTLS12}}},
	},nil
}

func (k *KueueBackend) request(ctx context.Context,method,path string,body any)([]byte,error){
	key,err:=os.ReadFile(k.tokenFile)
	if err!=nil{return nil,fmt.Errorf("Kubernetes identity: %w",err)}
	token:=strings.TrimSpace(string(key))
	if token==""{return nil,errors.New("empty Kubernetes identity")}
	var input io.Reader
	if body!=nil{
		buf,err:=json.Marshal(body)
		if err!=nil{return nil,err}
		input=bytes.NewReader(buf)
	}
	req,err:=http.NewRequestWithContext(ctx,method,k.endpoint+path,input)
	if err!=nil{return nil,err}
	req.Header.Set("Authorization","Bearer "+token)
	req.Header.Set("Accept","application/json")
	if body!=nil{req.Header.Set("Content-Type","application/json")}
	response,err:=k.client.Do(req)
	if err!=nil{return nil,err}
	defer response.Body.Close()
	raw,err:=io.ReadAll(io.LimitReader(response.Body,(1<<20)+1))
	if err!=nil{return nil,err}
	if len(raw)>1<<20{return nil,errors.New("Kubernetes response too large")}
	if response.StatusCode==404{return nil,errKubernetesNotFound}
	if response.StatusCode==409{return nil,errKubernetesConflict}
	if response.StatusCode<200||response.StatusCode>=300{
		return nil,fmt.Errorf("Kubernetes %s rejected with HTTP %d",method,response.StatusCode)
	}
	return raw,nil
}

func (k *KueueBackend) object(ctx context.Context,method,path string,body any)(map[string]any,error){
	raw,err:=k.request(ctx,method,path,body)
	if err!=nil{return nil,err}
	result:=map[string]any{}
	if len(raw)>0{
		if err:=json.Unmarshal(raw,&result);err!=nil{return nil,errors.New("malformed Kubernetes response")}
	}
	return result,nil
}

func batchKubernetesJobPath(name string)string{
	return "/apis/batch/v1/namespaces/"+batchNamespace+"/jobs/"+name
}

func batchKubernetesJob(row map[string]any)map[string]any{
	id:=asString(row["id"])
	cpu:=strconv.Itoa(int(math.Round(asFloat(row["cpu"])*1000)))+"m"
	mem:=strconv.Itoa(asInt(row["memory_mb"]))+"Mi"
	resource:=map[string]any{"cpu":cpu,"memory":mem}
	return map[string]any{
		"apiVersion":"batch/v1","kind":"Job",
		"metadata":map[string]any{
			"name":asString(row["remote_name"]),"namespace":batchNamespace,
			"labels":map[string]any{
				"kueue.x-k8s.io/queue-name":batchQueue,
				batchJobLabel:id,
				"app.kubernetes.io/managed-by":"project-workspace",
			},
			"annotations":map[string]any{
				"homelab.dbontr.me/project":asString(row["project"]),
				"homelab.dbontr.me/request-id":asString(row["request_id"]),
			},
		},
		"spec":map[string]any{
			"backoffLimit":0,"activeDeadlineSeconds":3600,"ttlSecondsAfterFinished":86400,
			"template":map[string]any{
				"metadata":map[string]any{"labels":map[string]any{batchJobLabel:id}},
				"spec":map[string]any{
					"restartPolicy":"Never","serviceAccountName":"pwm-batch-runner",
					"automountServiceAccountToken":false,"terminationGracePeriodSeconds":15,
					"securityContext":map[string]any{
						"runAsNonRoot":true,"runAsUser":65534,"runAsGroup":65534,
						"seccompProfile":map[string]any{"type":"RuntimeDefault"}},
					"containers":[]any{map[string]any{
						"name":"compute","image":asString(row["image"]),
						"imagePullPolicy":"IfNotPresent",
						"command":[]string{"/bin/sh","-c",asString(row["command"])},
						"resources":map[string]any{"requests":resource,"limits":resource},
						"securityContext":map[string]any{
							"allowPrivilegeEscalation":false,
							"readOnlyRootFilesystem":true,
							"capabilities":map[string]any{"drop":[]string{"ALL"}},
						},
					}},
				},
			},
		},
	}
}

func asAnySlice(value any) []any {
	v, _ := value.([]any)
	return v
}

func matchBatchJob(job,row map[string]any)(string,error){
	meta:=decodeAnyObject(job["metadata"])
	if asString(meta["name"])!=asString(row["remote_name"])||
		asString(meta["namespace"])!=batchNamespace||
		asString(decodeAnyObject(meta["labels"])[batchJobLabel])!=asString(row["id"])||
		asString(decodeAnyObject(meta["labels"])["kueue.x-k8s.io/queue-name"])!=batchQueue||
		asString(decodeAnyObject(meta["annotations"])["homelab.dbontr.me/project"])!=asString(row["project"])||
		asString(decodeAnyObject(meta["annotations"])["homelab.dbontr.me/request-id"])!=asString(row["request_id"]){
		return "",errors.New("Kubernetes Job identity mismatch")
	}
	template:=decodeAnyObject(decodeAnyObject(job["spec"])["template"])
	pod:=decodeAnyObject(template["spec"])
	containers,_:=pod["containers"].([]any)
	if len(containers)!=1||len(asAnySlice(pod["initContainers"]))!=0{
		return "",errors.New("unexpected Job containers")
	}
	c:=decodeAnyObject(containers[0])
	cmd,_:=c["command"].([]any)
	if len(cmd)!=3||asString(cmd[0])!="/bin/sh"||asString(cmd[1])!="-c"||
		asString(cmd[2])!=asString(row["command"])||asString(c["image"])!=asString(row["image"]){
		return "",errors.New("Kubernetes Job execution spec mismatch")
	}
	cpu:=strconv.Itoa(int(math.Round(asFloat(row["cpu"])*1000)))+"m"
	memory:=strconv.Itoa(asInt(row["memory_mb"]))+"Mi"
	resources:=decodeAnyObject(c["resources"])
	for _,key:=range []string{"requests","limits"}{
		values:=decodeAnyObject(resources[key])
		if asString(values["cpu"])!=cpu||asString(values["memory"])!=memory{
			return "",errors.New("Kubernetes Job resource quota changed")
		}
	}
	podContext:=decodeAnyObject(pod["securityContext"])
	containerContext:=decodeAnyObject(c["securityContext"])
	capabilities:=decodeAnyObject(containerContext["capabilities"])
	if pod["automountServiceAccountToken"]!=false||
		asString(pod["serviceAccountName"])!="pwm-batch-runner"||
		podContext["runAsNonRoot"]!=true||
		asString(decodeAnyObject(podContext["seccompProfile"])["type"])!="RuntimeDefault"||
		containerContext["allowPrivilegeEscalation"]!=false||
		containerContext["readOnlyRootFilesystem"]!=true||
		len(asAnySlice(capabilities["drop"]))!=1||
		asString(asAnySlice(capabilities["drop"])[0])!="ALL"{
		return "",errors.New("Kubernetes Job security policy changed")
	}
	uid:=asString(meta["uid"])
	if uid==""{return "",errors.New("Kubernetes Job UID absent")}
	if prev:=asString(row["remote_uid"]);prev!=""&&prev!=uid{
		return "",errors.New("Kubernetes Job UID changed")
	}
	return uid,nil
}

func (k *KueueBackend) ensureJob(ctx context.Context,db *Database,row map[string]any)(map[string]any,error){
	name:=asString(row["remote_name"])
	obj,err:=k.object(ctx,"GET",batchKubernetesJobPath(name),nil)
	if errors.Is(err,errKubernetesNotFound){
		if asString(row["status"])!="kueue_queued"||asString(row["remote_uid"])!=""||asInt(row["create_attempted"])!=0{
			return nil,fmt.Errorf("%w: %s is absent after a recorded submission; verify the execution outcome before any retry",errKueueRecoveryRequired,name)
		}
		permitted,beginErr:=db.beginKueueSubmission(asString(row["id"]))
		if beginErr!=nil{return nil,beginErr}
		if !permitted{
			return nil,fmt.Errorf("%w: submission ownership is already recorded for %s",errKueueRecoveryRequired,name)
		}
		// The durable attempt flag is committed before the outbound POST.
		// A timeout must never reset it or create a second execution.
		obj,err=k.object(ctx,"POST","/apis/batch/v1/namespaces/"+batchNamespace+"/jobs",batchKubernetesJob(row))
		if errors.Is(err,errKubernetesConflict){
			obj,err=k.object(ctx,"GET",batchKubernetesJobPath(name),nil)
		}
	}
	if err!=nil{return nil,err}
	_,err=matchBatchJob(obj,row)
	return obj,err
}

func (k *KueueBackend) admitted(ctx context.Context,uid string)(bool,error){
	obj,err:=k.object(ctx,"GET","/apis/kueue.x-k8s.io/v1beta2/namespaces/"+batchNamespace+"/workloads",nil)
	if err!=nil{return false,err}
	items,_:=obj["items"].([]any)
	for _,item:=range items{
		w:=decodeAnyObject(item)
		owners,_:=decodeAnyObject(w["metadata"])["ownerReferences"].([]any)
		for _,owner:=range owners{
			if asString(decodeAnyObject(owner)["uid"])!=uid{continue}
			status:=decodeAnyObject(w["status"])
			conditions,_:=status["conditions"].([]any)
			for _,c:=range conditions{
				cd:=decodeAnyObject(c)
				if cd["type"]=="Admitted"&&cd["status"]=="True"{return true,nil}
			}
			return status["admission"]!=nil,nil
		}
	}
	return false,nil
}

func (k *KueueBackend) output(ctx context.Context,name,uid string)string{
	obj,err:=k.object(ctx,"GET","/api/v1/namespaces/"+batchNamespace+"/pods?labelSelector=job-name%3D"+name,nil)
	if err!=nil{return ""}
	items,_:=obj["items"].([]any)
	for _,item:=range items{
		pod:=decodeAnyObject(item)
		metadata:=decodeAnyObject(pod["metadata"])
		owners,_:=metadata["ownerReferences"].([]any)
		owned:=false
		for _,owner:=range owners{
			if asString(decodeAnyObject(owner)["uid"])==uid{owned=true}
		}
		if !owned{continue}
		podName:=asString(metadata["name"])
		if podName==""{continue}
		raw,err:=k.request(ctx,"GET","/api/v1/namespaces/"+batchNamespace+"/pods/"+podName+"/log?container=compute&limitBytes=16384",nil)
		if err==nil{
			if len(raw)>16384{raw=raw[:16384]}
			return string(raw)
		}
	}
	return ""
}

func (k *KueueBackend) reconcile(ctx context.Context,db *Database,row map[string]any)error{
	id,name:=asString(row["id"]),asString(row["remote_name"])
	if row["status"]=="cancel_requested"{
		job,err:=k.object(ctx,"GET",batchKubernetesJobPath(name),nil)
		if errors.Is(err,errKubernetesNotFound){
			result:="Kubernetes workload absent at cancellation"
			if asString(row["remote_uid"])=="" && asInt(row["create_attempted"])!=0 {
				result="Kubernetes workload absent; prior execution outcome unverified"
			}
			return db.updateKueueJob(id,asString(row["remote_uid"]),"cancelled",result)
		}
		if err!=nil{return err}
		uid,err:=matchBatchJob(job,row)
		if err!=nil{return err}
		_,err=k.object(ctx,"DELETE",batchKubernetesJobPath(name),map[string]any{
			"apiVersion":"v1","kind":"DeleteOptions","propagationPolicy":"Foreground",
			"preconditions":map[string]any{"uid":uid},
		})
		if errors.Is(err,errKubernetesNotFound){
			return db.updateKueueJob(id,uid,"cancelled","Kubernetes workload removal confirmed")
		}
		return err
	}
	job,err:=k.ensureJob(ctx,db,row)
	if err!=nil{
		if errors.Is(err,errKueueRecoveryRequired){
			return db.requireKueueRecovery(id,err.Error())
		}
		return err
	}
	uid:=asString(decodeAnyObject(job["metadata"])["uid"])
	// Fence a remote Job before querying Kueue; transient errors must not
	// cause an already-created job to be submitted again with a new UID.
	if err:=db.updateKueueJob(id,uid,"queued","");err!=nil{return err}
	status:=decodeAnyObject(job["status"])
	conditions,_:=status["conditions"].([]any)
	complete,failed:=false,false
	for _,c:=range conditions{
		condition:=decodeAnyObject(c)
		if condition["status"]!="True"{continue}
		if condition["type"]=="Complete"{complete=true}
		if condition["type"]=="Failed"{failed=true}
	}
	if complete||asInt(status["succeeded"])>0{
		result:=k.output(ctx,name,uid)
		if result==""{result="Kubernetes Job completed"}
		return db.updateKueueJob(id,uid,"succeeded",result)
	}
	if failed{
		result:=k.output(ctx,name,uid)
		if result==""{result="Kubernetes Job failed"}
		return db.updateKueueJob(id,uid,"failed",result)
	}
	if asInt(status["active"])>0{return db.updateKueueJob(id,uid,"running","")}
	admitted,err:=k.admitted(ctx,uid)
	if err!=nil{return err}
	if admitted{return db.updateKueueJob(id,uid,"admitted","")}
	return db.updateKueueJob(id,uid,"queued","")
}

func (s *Server) StartKueueReconciler(ctx context.Context){
	if s.Kueue==nil{return}
	go func(){
		tick:=time.NewTicker(5*time.Second)
		defer tick.Stop()
		for{
			pending,err:=s.DB.pendingKueueJobs()
			if err!=nil{log.Printf("Kueue queue read: %v",err)}
			for _,job:=range pending{
				select{case <-ctx.Done():return;default:}
				id:=asString(job["id"])
				bounded,cancel:=context.WithTimeout(ctx,15*time.Second)
				err=s.Kueue.reconcile(bounded,s.DB,job)
				cancel()
				if err!=nil{
					s.DB.kueueError(id,err)
					log.Printf("Kueue job %s: %v",id,err)
				}
			}
			select{case <-ctx.Done():return;case <-tick.C:}
		}
	}()
}
