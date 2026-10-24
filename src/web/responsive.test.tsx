/** @module ResponsiveLayoutTest @description Anchors the workspace's viewport ownership, pane-local scrolling, breakpoints, themes and safe areas. */
import {readFileSync} from "node:fs";
import {describe,expect,it} from "vitest";
import indexHtml from "./index.html?raw";

const css=(name:string)=>readFileSync(`src/web/${name}`,"utf8");
const theme=css("theme.css");
const shell=css("shell.css");
const conversation=css("conversation.css");
const inspector=css("inspector.css");
const responsive=css("responsive.css");
const dialogs=css("dialogs.css");
const rich=css("rich-content.css");

describe("workspace layout contract",()=>{
  it("owns one dynamic viewport with iPhone safe areas and no page scrolling",()=>{
    expect(indexHtml).toContain("viewport-fit=cover");
    expect(indexHtml).toContain('name="color-scheme" content="light dark"');
    expect(theme).toContain("html,body{margin:0;height:100%;overflow:hidden}");
    expect(shell).toContain(".app{height:100dvh;");
    expect(responsive).toContain("env(safe-area-inset-top)");
    expect(responsive).toContain("env(safe-area-inset-bottom)");
  });

  // Ported from D-20260924-051: iOS Safari compressed grid/flex library rows into overlapping cards.
  it("keeps library rows in normal block flow so a short phone viewport cannot compress them",()=>{
    const escaped=(value:string)=>value.replace(/[.*+?^${}()|[\]\\]/g,"\\$&");
    const rule=(selector:string,source:string)=>source.match(new RegExp(`(?:^|\n)${escaped(selector)}\\{([^}]*)\\}`))?.[1]??"";
    const list=rule(".session-list",shell);
    expect(list).toContain("overflow-y:auto");
    expect(list).not.toMatch(/display:(grid|flex)/);
    expect(rule(".session-item",shell)).toContain("padding:10px 8px");
    expect(rule(".session-item",shell)).not.toMatch(/flex:[^;]*1|flex-shrink:1|height:/);
    expect(responsive).not.toMatch(/\.session-list\{[^}]*display:(grid|flex)/);
  });

  it("scrolls only inside panes: the lens, the library, the inspector and dialogs",()=>{
    expect(shell).toContain(".main-scroll{overflow-y:auto;overflow-x:hidden;overscroll-behavior:contain;flex:1;min-height:0}");
    expect(shell).toContain(".session-list{overflow-y:auto;overscroll-behavior:contain;");
    expect(inspector).toContain(".inspector-content{overflow-y:auto;overscroll-behavior:contain;");
    expect(dialogs).toContain(".dialog-body{padding:13px 23px 24px;overflow:auto;overscroll-behavior:contain;");
    expect(conversation).toContain(".execution-list{");
    expect(conversation).toMatch(/\.execution-list\{[^}]*max-height:360px;overflow-y:auto/);
  });

  it("defines light and dark tokens that follow the system unless the viewer chose",()=>{
    expect(theme).toMatch(/^:root\{--bg:#f5f6f3;--paper:#fff;/m);
    expect(theme).toContain("@media (prefers-color-scheme:dark){:root:not([data-theme=light]){--bg:#1a1e1c;");
    expect(theme).toContain(":root[data-theme=dark]{--bg:#1a1e1c;");
    expect(theme).toContain("--accent:#22674e");
    expect(theme).toContain("--accent:#a2c8a6");
  });

  it("turns the inspector into an overlay, the library into a drawer, and drops the rail on phones",()=>{
    expect(responsive).toMatch(/@media \(max-width:1180px\)\{[\s\S]*?\.inspector\{display:none\}[\s\S]*?\.app\.inspector-overlay \.inspector\{display:flex;position:absolute/);
    expect(responsive).toMatch(/@media \(max-width:800px\)\{[\s\S]*?\.sidebar\{display:none\}[\s\S]*?\.app\.sidebar-open \.sidebar\{display:flex;position:fixed/);
    expect(responsive).toMatch(/@media \(max-width:540px\)\{[\s\S]*?\.rail\{display:none\}[\s\S]*?\.mobile-nav\{display:grid/);
  });

  it("keeps phone lens tabs in a grid rather than a sideways-scrolling strip",()=>{
    expect(responsive).toContain(".view-tabs{display:grid;grid-template-columns:repeat(4,minmax(0,1fr))");
    for(const sheet of [shell,responsive])expect(sheet).not.toMatch(/\.view-tabs\{[^}]*overflow-x:auto/);
    expect(responsive).not.toContain("scroll-snap-type: x");
  });

  it("hides the rail, library and inspector in focus mode while keeping reading width",()=>{
    expect(shell).toContain(".app.focus-mode .rail,.app.focus-mode .sidebar,.app.focus-mode .inspector{display:none}");
    expect(shell).toContain(".app.focus-mode .transcript{max-width:850px}");
  });

  it("sizes inspector content by the inspector's own width",()=>{
    expect(inspector).toContain("container:inspector/inline-size");
    expect(inspector).toContain("@container inspector (min-width:360px){.turn-metrics{grid-template-columns:repeat(3,minmax(0,1fr))}");
  });

  it("keeps the rich-content source toggle reachable on touch screens without covering text",()=>{
    expect(rich).toMatch(/\.richContent:hover>\.richToolbar,\.richContent:focus-within>\.richToolbar\{opacity:1\}/);
    expect(responsive).toMatch(/@media \(hover:none\)\{[\s\S]*?\.richContent \.richToolbar\{position:static;order:2/);
  });

  it("honours reduced motion",()=>{
    expect(responsive).toContain("@media (prefers-reduced-motion:reduce)");
  });
});
