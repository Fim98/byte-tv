// 交叉编译 Go 后端到 resources/bin/<os>/juku-backend[.exe]
// 客户端模式不依赖 Wails/cgo，CGO_ENABLED=0 即可交叉编译；macOS 产出 arm64+amd64 后用 lipo 合成通用二进制（需在 Mac 上执行 lipo）。
const { execFileSync } = require("node:child_process")
const fs = require("node:fs")
const path = require("node:path")

const root = path.resolve(__dirname, "../..")
const target = process.argv[2] || (process.platform === "win32" ? "windows" : process.platform)
const outDir = path.join(__dirname, "..", "resources", "bin", target === "windows" ? "win" : target === "darwin" ? "mac" : "linux")
fs.mkdirSync(outDir, { recursive: true })

function build(goos, goarch, output) {
  console.log(`go build ${goos}/${goarch} -> ${output}`)
  execFileSync("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", output, "."], {
    cwd: root,
    stdio: "inherit",
    env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "0" },
  })
}

if (target === "darwin") {
  const arm = path.join(outDir, "juku-backend-arm64")
  const amd = path.join(outDir, "juku-backend-amd64")
  build("darwin", "arm64", arm)
  build("darwin", "amd64", amd)
  if (process.platform === "darwin") {
    execFileSync("lipo", ["-create", "-output", path.join(outDir, "juku-backend"), arm, amd], { stdio: "inherit" })
    fs.rmSync(arm)
    fs.rmSync(amd)
  } else {
    console.log("非 macOS 主机：保留分架构二进制，请在 Mac 上用 lipo 合并为 juku-backend")
  }
} else if (target === "windows") {
  build("windows", "amd64", path.join(outDir, "juku-backend.exe"))
} else {
  build("linux", process.arch === "arm64" ? "arm64" : "amd64", path.join(outDir, "juku-backend"))
}
