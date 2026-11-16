import { describe, expect, it } from "vitest";
import manifestSource from "../../public/manifest.webmanifest?raw";
import indexHtml from "../../index.html?raw";

// ADR-108 §6: the manifest makes Chrome offer "Install app" (native-looking Android notifications).
// Its icons must exist as unhashed files under public/icons/ (Vite copies public/ verbatim).
const iconFiles = Object.keys(import.meta.glob("../../public/icons/*.png", { query: "?url" }));

interface Manifest {
  name: string;
  start_url: string;
  scope: string;
  display: string;
  icons: { src: string; sizes: string; type: string; purpose?: string }[];
}

const manifest = JSON.parse(manifestSource) as Manifest;

describe("manifest.webmanifest (ADR-108 §6)", () => {
  it("is a standalone app named Inwestycje that starts on /portfolio with scope /", () => {
    expect(manifest.name).toBe("Inwestycje");
    expect(manifest.start_url).toBe("/portfolio");
    expect(manifest.scope).toBe("/");
    expect(manifest.display).toBe("standalone");
  });

  it("declares PNG icons at 192 and 512 px (Chrome's install criteria) that exist on disk", () => {
    const sizes = manifest.icons.map((i) => i.sizes).sort();
    expect(sizes).toEqual(["192x192", "512x512"]);
    for (const icon of manifest.icons) {
      expect(icon.type).toBe("image/png");
      expect(icon.src.startsWith("/icons/")).toBe(true);
      expect(iconFiles.some((f) => f.endsWith(icon.src))).toBe(true);
    }
  });

  it("ships the notification icon and the monochrome status-bar badge the worker references", () => {
    for (const name of ["icon-192.png", "badge-96.png"]) {
      expect(iconFiles.some((f) => f.endsWith(`/icons/${name}`))).toBe(true);
    }
  });
});

describe("index.html", () => {
  it("links the manifest and sets theme-color", () => {
    expect(indexHtml).toContain('<link rel="manifest" href="/manifest.webmanifest" />');
    expect(indexHtml).toMatch(/<meta name="theme-color" content="#[0-9A-Fa-f]{6}" \/>/);
  });
});
