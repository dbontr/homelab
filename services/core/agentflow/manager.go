package agentflow

// Manager exposes engineering run lifecycle operations without granting a
// caller access to the compute service, model credentials or privileged hosts.
type Manager interface {
 Submit(Program)(RunSummary,error)
 Get(string)(RunSummary,error)
 List()([]RunSummary,error)
 Cancel(string,uint64)error
}
