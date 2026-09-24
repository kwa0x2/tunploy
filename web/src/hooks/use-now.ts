import { useEffect, useState } from "react"

// useNow re-renders on a timer so "seen 2m ago" keeps counting between polls.
export function useNow(intervalMs = 10_000) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(timer)
  }, [intervalMs])
  return now
}
