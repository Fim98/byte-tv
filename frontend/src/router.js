import { useEffect, useState } from "react"

export function parseHash() {
  const raw = window.location.hash.replace(/^#/, "") || "/home"
  const [path, query = ""] = raw.split("?")
  return { path, parts: path.split("/").filter(Boolean), query: new URLSearchParams(query) }
}

export function navigate(path) {
  if (window.location.hash !== "#" + path) window.location.hash = path
}

export function useRoute() {
  const [route, setRoute] = useState(parseHash)
  useEffect(() => {
    const update = () => setRoute(parseHash())
    window.addEventListener("hashchange", update)
    return () => window.removeEventListener("hashchange", update)
  }, [])
  return route
}
