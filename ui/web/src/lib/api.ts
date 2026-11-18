import { useEffect, useState } from "react"

export type JSONRecord = Record<string, unknown>
export type Session = { authenticated: boolean; csrf_token?: string | null }
export type Resource<T> = {
  data: T | null
  error: string | null
  loading: boolean
}

function apiPath(path: string): string {
  if (!path.startsWith("/") || path.startsWith("//") || path.includes("\\") || path.includes("://")) {
    throw new Error("Invalid same-origin API path")
  }
  return path
}

function responseError(code: number): string {
  switch (code) {
    case 401: return "Authentication required"
    case 403: return "Access denied"
    case 404: return "Resource not found"
    case 409: return "The record has changed or is in use"
    case 428: return "Current record revision required"
    case 503: return "Service unavailable or not configured"
    default: return `The service returned HTTP ${code}`
  }
}

export async function apiGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(apiPath(path), {
    method: "GET",
    credentials: "same-origin",
    redirect: "error",
    headers: { Accept: "application/json" },
    signal,
  })
  if (!response.ok) throw new Error(responseError(response.status))
  try { return await response.json() as T }
  catch { throw new Error("The service returned invalid JSON") }
}

export async function apiWrite<T>(
  method: "POST" | "PUT" | "DELETE",
  path: string,
  csrf: string,
  payload: unknown,
): Promise<T> {
  if (!csrf) throw new Error("A verified browser session is required")
  const response = await fetch(apiPath(path), {
    method,
    credentials: "same-origin",
    redirect: "error",
    headers: {
      "Content-Type": "application/json",
      "Accept": "application/json",
      "X-CSRF-Token": csrf,
    },
    body: JSON.stringify(payload),
  })
  if (!response.ok) throw new Error(responseError(response.status))
  try { return await response.json() as T }
  catch { throw new Error("The service returned invalid JSON") }
}

export function useApi<T>(path: string, revision = 0): Resource<T> {
  const [value, setValue] = useState<Resource<T>>({ data: null, error: null, loading: true })
  useEffect(() => {
    if (!path) {
      setValue({ data: null, error: null, loading: false })
      return
    }
    const controller = new AbortController()
    setValue({ data: null, error: null, loading: true })
    apiGet<T>(path, controller.signal).then(
      data => { if (!controller.signal.aborted) setValue({ data, error: null, loading: false }) },
      (error: unknown) => {
        if (!controller.signal.aborted) setValue({
          data: null,
          error: error instanceof Error ? error.message : "The request failed",
          loading: false,
        })
      }
    )
    return () => controller.abort()
  }, [path, revision])
  return value
}

export function asRecord(value: unknown): JSONRecord | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as JSONRecord : null
}

export function list(value: unknown, keys: string[] = ["items", "results", "projects", "runs"]): JSONRecord[] {
  if (Array.isArray(value)) return value.map(asRecord).filter((v): v is JSONRecord => v !== null)
  const record = asRecord(value)
  if (!record) return []
  for (const key of keys) {
    if (Array.isArray(record[key])) {
      return (record[key] as unknown[]).map(asRecord).filter((v): v is JSONRecord => v !== null)
    }
  }
  return []
}

export function text(item: JSONRecord, ...keys: string[]): string {
  for (const key of keys) {
    const v = item[key]
    if (typeof v === "string" && v.trim() !== "") return v
    if (typeof v === "number" && Number.isFinite(v)) return String(v)
  }
  return ""
}

export function validProject(id: string): boolean {
  return /^[a-z0-9][a-z0-9_-]{0,79}$/.test(id)
}

export function statusVariant(value: string): "secondary" | "destructive" | "outline" {
  const s = value.toLowerCase()
  if (["failed", "unhealthy", "error", "denied", "firing"].includes(s)) return "destructive"
  if (["succeeded", "healthy", "ready", "active", "completed"].includes(s)) return "outline"
  return "secondary"
}
