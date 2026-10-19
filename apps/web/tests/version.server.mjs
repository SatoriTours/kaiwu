import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const port = process.env.KAIWU_VERSION_TEST_PORT || "17551";
// The fixture's own revision defaults to a fixed value, but the deployment
// pipeline passes the candidate revision so the suite can validate any commit.
const FIXTURE_REVISION = "8b54309f03dd75759291be48ff3f3b766ab89b5e";
const RAW_REVISION = (process.env.KAIWU_VERSION_TEST_REVISION || "").trim();
const REVISION = /^[0-9a-f]{40}$/i.test(RAW_REVISION)
  ? RAW_REVISION.toLowerCase()
  : FIXTURE_REVISION;
const dir = mkdtempSync(join(tmpdir(), "kaiwu-version-service-"));
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
  try {
    rmSync(dir, { recursive: true, force: true });
  } catch {}
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

  const ldflags = [
    `-X github.com/SatoriTours/kaiwu/server/internal/buildinfo.revision=${REVISION}`,
    "-X github.com/SatoriTours/kaiwu/server/internal/buildinfo.summary=版本可见性与部署状态",
    "-X github.com/SatoriTours/kaiwu/server/internal/buildinfo.builtAt=2026-10-19T01:02:03Z",
  ].join(" ");

  execFileSync(
    "go",
    [
      "-C",
      join(root, "server"),
      "build",
      "-ldflags",
      ldflags,
      "-o",
      binary,
      "./cmd/kaiwu-server",
    ],
    { env, stdio: "inherit" },
  );

  const dataDir = join(dir, "data");
  mkdirSync(dataDir, { recursive: true });

  const statusContent = {
    revision: REVISION,
    deployedAt: "2026-10-19T01:03:00Z",
    deployment: {
      revision: REVISION,
      status: "succeeded",
      at: "2026-10-19T01:03:00Z",
    },
  };
  writeFileSync(join(dataDir, "deployment-status.json"), JSON.stringify(statusContent));

  const project = join(dir, "project");
  execFileSync("git", ["init", project], { stdio: "ignore" });

  child = spawn(binary, [], {
    env: {
      ...env,
      KAIWU_WEB_TOKEN: "kaiwu-version-test-token-00000000",
      KAIWU_DATA_DIR: dataDir,
      KAIWU_LISTEN_ADDR: `127.0.0.1:${port}`,
      KAIWU_PI_PROGRAM: fake,
      KAIWU_PI_PROVIDER: "fixture",
      KAIWU_PI_MODEL: "isolated-test",
      PI_CODING_AGENT_DIR: join(dir, "personal-pi-fixture"),
    },
    stdio: "inherit",
  });

  const stop = () => {
    if (child) {
      child.kill("SIGTERM");
      setTimeout(() => {
        try {
          child.kill("SIGKILL");
        } catch {}
      }, 8000).unref();
    }
  };

  process.on("SIGTERM", stop);
  process.on("SIGINT", stop);

  child.on("exit", (code) => {
    cleanup();
    process.exit(code ?? 0);
  });
} catch (e) {
  cleanup();
  console.error("Failed to start version test server:", e);
  process.exit(1);
}
