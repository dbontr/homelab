package motivation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

type Engine struct {
	mu sync.Mutex
	path string
	lock *os.File
	state Record
}

func Open(path string)(*Engine,error){
	if !filepath.IsAbs(path)||filepath.Base(path)=="." {
		return nil,InvalidError("motivation state path must be absolute")
	}
	directory,err:=filepath.EvalSymlinks(filepath.Dir(path))
	if err!=nil{return nil,err}
	dir,err:=os.Stat(directory)
	if err!=nil||!dir.IsDir(){return nil,InvalidError("motivation state directory unavailable")}
	actual:=filepath.Join(directory,filepath.Base(path))
	if err:=checkPrivateRegular(actual,true);err!=nil{return nil,err}
	lockPath:=actual[:len(actual)-len(filepath.Ext(actual))]+".lock"
	if err:=checkPrivateRegular(lockPath,true);err!=nil{return nil,err}
	lock,err:=os.OpenFile(lockPath,os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0600)
	if err!=nil{return nil,err}
	if err:=syscall.Flock(int(lock.Fd()),syscall.LOCK_EX|syscall.LOCK_NB);err!=nil{
		lock.Close()
		return nil,ConflictError("motivation state is held by another process")
	}
	release:=func(err error)(*Engine,error){
		syscall.Flock(int(lock.Fd()),syscall.LOCK_UN)
		lock.Close()
		return nil,err
	}
	if err:=checkPrivateDescriptor(lock);err!=nil{return release(err)}
	record:=defaultRecord()
	data,err:=readState(actual)
	if errors.Is(err,os.ErrNotExist){
		return &Engine{path:actual,lock:lock,state:record},nil
	}
	if err!=nil{return release(err)}
	if err:=decodeState(data,&record);err!=nil{return release(err)}
	if err:=validState(record);err!=nil{return release(err)}
	return &Engine{path:actual,lock:lock,state:record},nil
}

func checkPrivateRegular(path string,allowMissing bool)error{
	info,err:=os.Lstat(path)
	if errors.Is(err,os.ErrNotExist)&&allowMissing{return nil}
	if err!=nil{return err}
	if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{
		return InvalidError("motivation state and lock must be private regular files")
	}
	return nil
}

func checkPrivateDescriptor(file *os.File)error{
	info,err:=file.Stat()
	if err!=nil{return err}
	if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0{return InvalidError("insecure state descriptor")}
	return nil
}

func readState(path string)([]byte,error){
	file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0)
	if err!=nil{return nil,err}
	defer file.Close()
	if err:=checkPrivateDescriptor(file);err!=nil{return nil,err}
	info,err:=file.Stat()
	if err!=nil{return nil,err}
	if info.Size()>maxStateBytes{return nil,InvalidError("motivation state exceeds capacity")}
	data,err:=io.ReadAll(io.LimitReader(file,maxStateBytes+1))
	if err!=nil{return nil,err}
	if len(data)>maxStateBytes{return nil,InvalidError("motivation state exceeds capacity")}
	return data,nil
}

func decodeState(data []byte,record *Record)error{
	decoder:=json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err:=decoder.Decode(record);err!=nil{return InvalidError("motivation state is not valid JSON")}
	var extra any
	if err:=decoder.Decode(&extra);err!=io.EOF{return InvalidError("motivation state contains trailing data")}
	return nil
}

func cloneState(record Record)Record{
	clone:=record
	clone.Interventions=append([]Intervention{},record.Interventions...)
	clone.Checkins=append([]Checkin{},record.Checkins...)
	clone.FocusSessions=append([]FocusSession{},record.FocusSessions...)
 clone.AttentionPlans=append([]AttentionPlan{},record.AttentionPlans...)
 clone.CuriosityPrompts=append([]CuriosityPrompt{},record.CuriosityPrompts...)
	for i:=range clone.Interventions{
		if f:=record.Interventions[i].Feedback;f!=nil{
			value:=*f
			clone.Interventions[i].Feedback=&value
		}
	}
	for i:=range clone.Checkins{
		if x:=record.Checkins[i].InterventionID;x!=nil{
			value:=*x
			clone.Checkins[i].InterventionID=&value
		}
	}
	for i:=range clone.FocusSessions{
		if x:=record.FocusSessions[i].EndedAt;x!=nil{
			value:=*x
			clone.FocusSessions[i].EndedAt=&value
		}
	}
	if x:=record.Profile.HoldoutWeekdayUTC;x!=nil{
		value:=*x
		clone.Profile.HoldoutWeekdayUTC=&value
	}
	return clone
}

func(e *Engine) Snapshot()Record{
	e.mu.Lock()
	defer e.mu.Unlock()
	return cloneState(e.state)
}

func(e *Engine) commit(next Record)(Record,error){
	if err:=validState(next);err!=nil{return Record{},err}
	data,err:=json.MarshalIndent(next,"","  ")
	if err!=nil||len(data)>maxStateBytes{
		return Record{},InvalidError("motivation state exceeds capacity")
	}
	dir:=filepath.Dir(e.path)
	temp,err:=os.CreateTemp(dir,".motivation-*.tmp")
	if err!=nil{return Record{},err}
	defer os.Remove(temp.Name())
	if err:=temp.Chmod(0600);err!=nil{temp.Close();return Record{},err}
	if _,err:=temp.Write(data);err!=nil{temp.Close();return Record{},err}
	if err:=temp.Sync();err!=nil{temp.Close();return Record{},err}
	if err:=temp.Close();err!=nil{return Record{},err}
	if err:=os.Rename(temp.Name(),e.path);err!=nil{return Record{},err}
	parent,err:=os.Open(dir)
	if err!=nil{return Record{},err}
	syncErr:=parent.Sync()
	closeErr:=parent.Close()
	if syncErr!=nil{return Record{},syncErr}
	if closeErr!=nil{return Record{},closeErr}
	e.state=cloneState(next)
	return cloneState(e.state),nil
}

func(e *Engine) Close()error{
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lock==nil{return nil}
	err:=syscall.Flock(int(e.lock.Fd()),syscall.LOCK_UN)
	closeErr:=e.lock.Close()
	e.lock=nil
	if err!=nil{return err}
	return closeErr
}
