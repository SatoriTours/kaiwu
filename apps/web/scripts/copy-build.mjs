import { cp, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
const destination = fileURLToPath(
  new URL("../../../server/internal/webui/dist/", import.meta.url),
);
await mkdir(destination, { recursive: true });
await cp(fileURLToPath(new URL("../dist/", import.meta.url)), destination, {
  recursive: true,
});
