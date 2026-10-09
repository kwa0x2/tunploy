export interface ApiErrorBody {
  code: string
  message: string
  fields?: Record<string, string>
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly fields: Record<string, string>

  constructor(status: number, body: ApiErrorBody) {
    super(body.message)
    this.name = "ApiError"
    this.status = status
    this.code = body.code
    this.fields = body.fields ?? {}
  }
}

// log is the container's last output, gone once the server rolls back.
export class DeployError extends ApiError {
  readonly log: string[]

  constructor(body: ApiErrorBody, log: string[] = []) {
    super(502, body)
    this.name = "DeployError"
    this.log = log
  }
}

export interface Page<T> {
  data: T[]
  has_more: boolean
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      credentials: "same-origin",
      headers: init?.body ? { "Content-Type": "application/json" } : undefined,
      ...init,
    })
  } catch {
    throw new ApiError(0, {
      code: "network_error",
      message: "Cannot reach the server. Is Tunploy running?",
    })
  }

  if (res.status === 204) return undefined as T

  const text = await res.text()
  let payload: unknown = null
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      throw new ApiError(res.status, {
        code: "invalid_response",
        message: "The server returned a response we could not read.",
      })
    }
  }

  if (!res.ok) {
    const body = (payload as { error?: ApiErrorBody } | null)?.error
    throw new ApiError(
      res.status,
      body ?? { code: "unknown_error", message: `Request failed with status ${res.status}` },
    )
  }

  return payload as T
}

export const post = <T,>(path: string, body?: unknown) =>
  request<T>(path, { method: "POST", body: body ? JSON.stringify(body) : undefined })

export const patch = <T,>(path: string, body: unknown) =>
  request<T>(path, { method: "PATCH", body: JSON.stringify(body) })

export const put = <T,>(path: string, body: unknown) =>
  request<T>(path, { method: "PUT", body: JSON.stringify(body) })

export const del = (path: string) => request<void>(path, { method: "DELETE" })

async function fetchRaw(path: string, init?: RequestInit): Promise<Response> {
  let res: Response
  try {
    res = await fetch(path, { credentials: "same-origin", ...init })
  } catch (err) {
    if (init?.signal?.aborted) throw err
    throw new ApiError(0, { code: "network_error", message: "Cannot reach the server." })
  }
  if (!res.ok) {
    const body = await res
      .json()
      .then((p: { error?: ApiErrorBody }) => p.error)
      .catch(() => undefined)
    throw new ApiError(
      res.status,
      body ?? { code: "unknown_error", message: `Request failed with status ${res.status}` },
    )
  }
  return res
}

export const fetchText = async (path: string) => (await fetchRaw(path)).text()

export async function streamText(path: string, onChunk: (text: string) => void, init?: RequestInit) {
  const res = await fetchRaw(path, init)
  if (!res.body) return
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  for (;;) {
    const { done, value } = await reader.read()
    if (done) return
    onChunk(value)
  }
}

export interface StreamEvent<T, S> {
  step?: S
  error?: ApiErrorBody
  log?: string[]
  result?: T
}

// NDJSON: steps as they happen, then the result or an error on the last line.
export async function streamSteps<T, S>(
  path: string,
  body: unknown,
  parse: (raw: Record<string, unknown>) => StreamEvent<T, S>,
  onStep: (step: S) => void,
): Promise<T> {
  let result: T | undefined
  let rest = ""

  const handle = (line: string) => {
    if (!line.trim()) return
    let event: StreamEvent<T, S>
    try {
      event = parse(JSON.parse(line) as Record<string, unknown>)
    } catch {
      throw new ApiError(0, {
        code: "invalid_response",
        message: "The server returned a response we could not read.",
      })
    }
    if (event.step) onStep(event.step)
    if (event.error) throw new DeployError(event.error, event.log)
    if (event.result) result = event.result
  }

  await streamText(
    path,
    (chunk) => {
      const lines = (rest + chunk).split("\n")
      rest = lines.pop() ?? ""
      lines.forEach(handle)
    },
    {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/x-ndjson" },
      body: JSON.stringify(body),
    },
  )
  handle(rest)

  if (!result) {
    throw new ApiError(0, {
      code: "stream_ended",
      message: "The connection closed before it finished. Check the list before trying again.",
    })
  }
  return result
}
