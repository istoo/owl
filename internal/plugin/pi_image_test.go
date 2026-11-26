package plugin

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// An image a request names by URL is downloaded only from the public
// internet, and only up to a cap: a loopback address, the cloud metadata
// service, an address in the user's own network or named inside IPv6,
// another scheme, and a body past the cap are all refused (#35).
func TestPiImageFetchRefusesPrivateAndHuge(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on PATH")
	}
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", piImageSafetyJS, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("pi.js's image fetch: %v\n%s", err, out)
	}
	if len(out) > 0 {
		t.Logf("%s", out)
	}
}

const piImageSafetyJS = `
import assert from "node:assert/strict";
import path from "node:path";
import { pathToFileURL } from "node:url";
const dir = process.argv[1];
const pi = await import(pathToFileURL(path.join(dir, "pi.js")).href);

// this computer, its network, link-local (cloud metadata), CGNAT, and an
// address named inside IPv6 are all refused before a request is sent
for (const url of [
  "http://127.0.0.1:9/x.png",
  "http://169.254.169.254/latest/meta-data/",
  "http://[::1]:9/x.png",
  "http://[::ffff:127.0.0.1]:9/x.png",
  "http://[fd00::1]:9/x.png",
  "http://[fe80::1]:9/x.png",
  "http://10.0.0.1/x.png",
  "http://172.16.5.5/x.png",
  "http://192.168.1.1/x.png",
  "http://100.64.0.1/x.png",
  "http://0.0.0.0:9/x.png",
]) {
  await assert.rejects(() => pi.safeImageFetch(url), /private address/, url);
}
// another scheme is no image URL, and neither is a file on this computer
await assert.rejects(() => pi.safeImageFetch("file:///etc/passwd"), /http\(s\)/, "file scheme");
await assert.rejects(() => pi.safeImageFetch("ftp://example.com/x.png"), /http\(s\)/, "ftp scheme");

// the address classes, on their own
for (const [ip, want] of [
  ["127.0.0.1", true], ["10.1.2.3", true], ["172.31.0.1", true], ["192.168.0.1", true],
  ["169.254.169.254", true], ["100.64.0.1", true], ["100.128.0.1", false], ["0.0.0.0", true],
  ["255.255.255.255", true], ["::1", true], ["::", true], ["::ffff:127.0.0.1", true],
  ["::ffff:8.8.8.8", false], ["fd00::1", true], ["fe80::1", true], ["2001:4860:4860::8888", false],
  ["8.8.8.8", false], ["1.1.1.1", false],
]) {
  assert.equal(pi.privateAddress(ip), want, ip);
}

// a body past the cap is refused, by its Content-Length and by counting
// the stream when there is none
const declared = new Response("x", { headers: { "content-length": String(21 * 1024 * 1024) } });
await assert.rejects(() => pi.readCapped(declared), /more than/, "a length past the cap");
let sent = 0;
const endless = new Response(new ReadableStream({
  pull(c) {
    c.enqueue(new Uint8Array(1024 * 1024));
    if (++sent > 30) c.close();
  },
}));
await assert.rejects(() => pi.readCapped(endless), /larger than/, "a stream past the cap");

// an image under the cap comes back whole
const small = new Response(new Uint8Array([1, 2, 3]), { headers: { "content-type": "image/png" } });
assert.equal((await pi.readCapped(small)).length, 3);
console.log("pi image safety ok");
process.exit(0);
`
