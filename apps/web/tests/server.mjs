import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
const dir = mkdtempSync(join(tmpdir(), "kaiwu-web-service-"));
const env = {
  ...process.env,
  GOTOOLCHAIN: "local",
  GOCACHE: "/tmp/kaiwu-go-cache",
  GOMODCACHE: "/tmp/kaiwu-go-mod-cache",
};
const root = resolve("../.."),
  fake = join(dir, "fakepi"),
  binary = join(dir, "kaiwu-server");
let child;
let cleaned = false;
function cleanup() {
  if (cleaned) return;
  cleaned = true;
  rmSync(dir, { recursive: true, force: true });
}
try {
  execFileSync(
    "go",
    [
      "-C",
      join(root, "server"),
      "build",
      "-o",
      fake,
      "./internal/agent/testdata/fakepi",
    ],
    { env, stdio: "inherit" },
  );
  execFileSync(
    "go",
    ["-C", join(root, "server"), "build", "-o", binary, "./cmd/kaiwu-server"],
    { env, stdio: "inherit" },
  );
  const project = join(dir, "project");
  execFileSync("git", ["init", project], { stdio: "ignore" });
  writeFileSync(
    join(root, ".superpowers/sdd/2026-10-11-web-service/browser-project.txt"),
    project,
  );
  child = spawn(binary, [], {
    env: {
      ...env,
      KAIWU_WEB_TOKEN: "kaiwu-browser-test-token-00000000",
      KAIWU_DATA_DIR: join(dir, "data"),
      KAIWU_LISTEN_ADDR: "127.0.0.1:17331",
      KAIWU_PI_PROGRAM: fake,
      KAIWU_PI_PROVIDER: "fixture",
      KAIWU_PI_MODEL: "isolated-test",
      PI_CODING_AGENT_DIR: join(dir, "personal-pi-fixture"),
    },
    stdio: "inherit",
  });
  const stop = () => {
    child.kill("SIGTERM");
    setTimeout(() => child.kill("SIGKILL"), 8000).unref();
  };
  process.on("SIGTERM", stop);
  process.on("SIGINT", stop);
  child.on("exit", (code) => {
    cleanup();
    process.exit(code ?? 1);
  });
} catch (e) {
  console.error(e);
  cleanup();
  process.exit(1);
}
