/** @module FormatTest @description Covers UI metric formatting boundaries. */
import {describe,expect,it} from "vitest";
import {absoluteTime,clockTime,compact,duration,elapsed,excerpt,percent,plural,shortDate,shortDateTime,span,twoDigits} from "./format";
describe("format",()=>{
  it("formats tokens, ratios, and time",()=>{expect(compact(142000)).toMatch(/142/);expect(percent(.614)).toBe("61%");expect(duration(32*60000)).toBe("32m");expect(duration(68*60000)).toBe("1h 8m");expect(duration(0)).toBe("—");expect(elapsed("2026-01-01T00:00:00Z","2026-01-01T00:06:00Z")).toBe("00:06")});
  it("formats clock and date labels and reports unknown times",()=>{
    for(const format of [absoluteTime,clockTime,shortDateTime])expect(format("not a date")).toBe("Unknown time");
    expect(clockTime("2026-01-01T10:05:00Z")).toMatch(/\d{2}.\d{2}/);
    expect(shortDateTime("2026-01-01T10:05:00Z")).toMatch(/2026/);
    expect(shortDate("2026-01-01T10:05:00Z")).toMatch(/2026/);
    expect(shortDate("2026-01-01T10:05:00Z")).not.toMatch(/\d{1,2}:\d{2}/);
    expect(shortDate("not a date")).toBe("Unknown date");
    expect(span("2026-01-01T10:00:00Z","2026-01-01T10:12:00Z")).toBe("12m");
    expect(span("2026-01-01T10:12:00Z","2026-01-01T10:00:00Z")).toBe("—");
    expect(twoDigits(3)).toBe("03");
    expect(twoDigits(12)).toBe("12");
  });
  it("counts nouns in the singular only for one",()=>{
    expect(plural(1,"session")).toBe("1 session");
    expect(plural(0,"session")).toBe("0 sessions");
    expect(plural(3,"session")).toBe("3 sessions");
  });
  it("makes one-line plain previews without Markdown markers",()=>{
    expect(excerpt("- **Cart** totals are `fixed`\n- second  line",80)).toBe("Cart totals are fixed second line");
    expect(excerpt("# Title\n```go\ncode()\n```\n1. step [link]",80)).toBe("Title: step link");
    expect(excerpt("## Checkout ##\n- Cart totals are now updated",80)).toBe("Checkout: Cart totals are now updated");
    expect(excerpt("### Done.\nAll green",80)).toBe("Done. All green");
    expect(excerpt("**Checkout**\n\n- Cart totals are now updated",80)).toBe("Checkout: Cart totals are now updated");
    expect(excerpt("Keep **bold** words inline\n**Done!**",80)).toBe("Keep bold words inline Done!");
    expect(excerpt("abcdefghij",5)).toBe("abcd…");
    expect(excerpt("abcde",5)).toBe("abcde");
    expect(excerpt(undefined,5)).toBe("");
    expect(excerpt("| a | b |",20)).toBe("a b");
    expect(excerpt("> Opened **PR #482** for cart_test.go",60)).toBe("Opened PR #482 for cart_test.go");
  });
});
