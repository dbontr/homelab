package approval

import "strings"

// ScopeFor classifies cluster-changing operations that need explicit approval.
func ScopeFor(name string) string {
    if strings.HasPrefix(name, "cluster__") || strings.HasPrefix(name, "infrastructure__") {
        return "cluster.modify"
    }
    if strings.HasPrefix(name, "health__") || strings.HasPrefix(name, "learning__") {
        tool := name[strings.Index(name, "__")+2:]
        for _, prefix := range []string{"delete_", "purge_", "reset_", "admin_", "credential_", "grant_", "revoke_"} {
            if strings.HasPrefix(tool, prefix) { return "personal.data.manage" }
        }
        return ""
    }
    if !strings.HasPrefix(name, "workspace__") {
        return ""
    }
    tool := strings.TrimPrefix(name, "workspace__")
    // Routine personal operations and inspection are available without a second unlock.
    switch tool {
    case "list_projects", "project_published_library", "read_project_note",
        "search_project_knowledge", "knowledge_policy", "knowledge_git_read",
        "knowledge_context", "knowledge_contributions", "knowledge_contribution",
        "knowledge_search_contributions", "list_compute_nodes",
        "task_worker_access", "list_project_jobs", "git_provider_capabilities",
        "git_repository_head", "git_read_file", "list_github_repositories",
        "task_list", "task_details", "task_files", "task_file_info",
        "task_read_base64", "task_compute_jobs", "personal_task_claim_list",
        "personal_task_claim_status", "task_claim_list", "task_claim_status",
        "resource_plan", "workspace_list", "workspace_operation", "list_hardware",
        "environment_list", "pool_list_images", "pool_list_software",
        "pool_list_programs", "pool_entry_info", "pool_events",
        "workspace_snapshot_list", "workspace_checkpoint_stop_status",
        "external_resource_list", "external_resource_leases",
        "github_account_search", "github_account_repositories",
        "github_account_repository", "github_account_settings",
        "github_pat_readiness", "github_repository", "github_read_file",
        "github_list_issues", "github_list_pull_requests", "github_get_issue",
        "github_list_branches", "github_get_pull_request",
        "github_verify_pull_request", "list_project_artifacts",
        "artifact_info", "artifact_read_base64", "artifact_view_image",
        "github_project_board", "github_project_items", "github_project_fields",
        "repository_memberships", "research_sources", "research_search",
        "research_topics", "research_browse_topic", "research_read_passage",
        "library_list", "library_sources", "library_resolve", "library_read_file",
        "create_project", "create_library_project", "write_project_note",
        "knowledge_configure", "knowledge_contribute", "link_github_repository",
        "git_link_repository", "git_checkout_repository", "task_create",
        "task_update", "task_save_text", "task_attach_base64",
        "task_study_pack_import", "task_send_base64", "upload_begin",
        "upload_chunk", "upload_status", "upload_finish", "upload_cancel",
        "task_export_project", "study_pack_prepare", "research_register_source",
        "research_ingest_passages", "library_create", "library_add_source",
        "library_link", "library_import_object", "library_checkout_source":
        return ""
    }
    // Core infrastructure, upstream software catalogs, and GitHub account changes
    // are never authorized solely by possession of the gateway bearer token.
    switch tool {
    case "worker_revoke", "task_worker_authorize", "task_hardware_recover",
        "external_resource_register", "external_resource_reserve",
        "external_resource_release":
        return "cluster.modify"
    case "pool_register_image", "pool_register_software", "pool_propose_program",
        "pool_review_program", "pool_update_entry", "environment_define",
        "environment_define_from_pool", "environment_build",
        "environment_import", "environment_export":
        return "software.manage"
    case "workspace_call", "workspace_create", "workspace_destroy",
        "workspace_capture_display", "workspace_import_artifact",
        "workspace_snapshot", "workspace_checkpoint_stop",
        "workspace_prune_volume", "workspace_restore", "submit_compute_job",
        "task_compute_submit", "personal_task_claim", "personal_task_claim_renew",
        "personal_task_claim_finish", "task_claim", "task_claim_renew",
        "task_claim_finish":
        return "compute.execute"
    }
    if strings.HasPrefix(tool, "github_") ||
        strings.HasPrefix(tool, "github_account_") ||
        strings.HasPrefix(tool, "git_publish") ||
        strings.HasPrefix(tool, "knowledge_git_publish") {
        return "source.modify"
    }
    if strings.HasPrefix(tool, "pool_") || strings.HasPrefix(tool, "environment_") {
        return "software.manage"
    }
    if strings.HasPrefix(tool, "external_resource_") || strings.HasPrefix(tool, "worker_") {
        return "cluster.modify"
    }
    // Unrecognized workspace operations require explicit approval by default.
    return "workspace.modify"
}

