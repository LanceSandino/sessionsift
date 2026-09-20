/** @module FormatTest @description Covers UI metric formatting boundaries. */
import {describe,expect,it} from "vitest";
import {compact,duration,elapsed,percent} from "./format";
describe("format",()=>{it("formats tokens, ratios, and time",()=>{expect(compact(142000)).toMatch(/142/);expect(percent(.614)).toBe("61%");expect(duration(32*60000)).toBe("32m");expect(duration(68*60000)).toBe("1h 8m");expect(duration(0)).toBe("—");expect(elapsed("2026-01-01T00:00:00Z","2026-01-01T00:06:00Z")).toBe("00:06")})});
