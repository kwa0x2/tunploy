import { useCallback, useEffect, useRef, useState } from "react"

// useResource loads data once and, with pollMs, keeps it fresh while the tab
// is visible. Only the newest request may write state, so a slow poll can't
// overwrite what a mutation just reloaded.
export function useResource<T>(load: () => Promise<T>, pollMs?: number) {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<unknown>()
  const [loading, setLoading] = useState(true)
  const latest = useRef(0)

  const reload = useCallback(async () => {
    const id = ++latest.current
    try {
      const value = await load()
      if (id === latest.current) {
        setData(value)
        setError(undefined)
      }
    } catch (err) {
      if (id === latest.current) setError(err)
    } finally {
      if (id === latest.current) setLoading(false)
    }
  }, [load])

  useEffect(() => {
    void reload()
    if (!pollMs) return
    const timer = setInterval(() => {
      if (!document.hidden) void reload()
    }, pollMs)
    return () => clearInterval(timer)
  }, [reload, pollMs])

  return { data, error, loading, reload }
}
