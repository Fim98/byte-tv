import { useEffect, useState } from "react"

export function parseHash() {
  const raw = window.location.hash.replace(/^#/, "") || "/home"
  const [path, query = ""] = raw.split("?")
  return { path, parts: path.split("/").filter(Boolean), query: new URLSearchParams(query) }
}

const initialPath = typeof window !== "undefined" && window.location.hash
  ? window.location.hash.replace(/^#/, "") || "/home"
  : "/home"

let historyStack = [initialPath]
let historyIndex = 0
const listeners = new Set()

function notify() {
  listeners.forEach((fn) => fn())
}

export function navigate(path) {
  const target = path.startsWith("/") ? path : "/" + path
  if (window.location.hash !== "#" + target) {
    historyStack = historyStack.slice(0, historyIndex + 1).concat(target)
    historyIndex = historyStack.length - 1
    window.location.hash = target
    notify()
  }
}

export function goBack() {
  if (historyIndex > 0) {
    historyIndex--
    window.location.hash = historyStack[historyIndex]
    notify()
  } else if (typeof window !== "undefined" && window.history.length > 1) {
    window.history.back()
  }
}

export function goForward() {
  if (historyIndex < historyStack.length - 1) {
    historyIndex++
    window.location.hash = historyStack[historyIndex]
    notify()
  } else if (typeof window !== "undefined") {
    window.history.forward()
  }
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

export function useNavigation() {
  const [, setTick] = useState(0)
  useEffect(() => {
    const onUpdate = () => setTick((t) => t + 1)
    listeners.add(onUpdate)

    const onHashChange = () => {
      const current = window.location.hash.replace(/^#/, "") || "/home"
      if (historyIndex > 0 && historyStack[historyIndex - 1] === current) {
        historyIndex--
      } else if (historyIndex < historyStack.length - 1 && historyStack[historyIndex + 1] === current) {
        historyIndex++
      } else if (historyStack[historyIndex] !== current) {
        historyStack = historyStack.slice(0, historyIndex + 1).concat(current)
        historyIndex = historyStack.length - 1
      }
      onUpdate()
    }

    window.addEventListener("hashchange", onHashChange)
    return () => {
      listeners.delete(onUpdate)
      window.removeEventListener("hashchange", onHashChange)
    }
  }, [])

  return {
    canGoBack: historyIndex > 0,
    canGoForward: historyIndex < historyStack.length - 1,
    goBack,
    goForward,
  }
}
