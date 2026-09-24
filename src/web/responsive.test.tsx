/** @module ResponsiveLayoutTest @description Anchors viewport ownership and mobile safe-area behavior. */
import {readFileSync} from "node:fs";
import {describe,expect,it} from "vitest";
import indexHtml from "./index.html?raw";

const responsive=readFileSync("src/web/responsive.css","utf8");
const timeline=readFileSync("src/web/timeline.css","utf8");
const mobile=readFileSync("src/web/responsive-mobile.css","utf8");
const support=readFileSync("src/web/support.css","utf8");

describe("responsive layout contract",()=>{
  it("gives the remote support card the full overview width so codes and supporter names fit",()=>{
    expect(support).toContain(".overviewGrid .supportCard{grid-column:1/-1}");
  });

  it("uses device viewport units and iPhone safe areas",()=>{
    expect(indexHtml).toContain("viewport-fit=cover");
    expect(responsive).toContain("height: 100dvh");
    expect(mobile).toContain("env(safe-area-inset-bottom)");
    expect(mobile).toContain("env(safe-area-inset-top)");
  });

  it("defines distinct tablet and mobile layout regimes",()=>{
    expect(responsive).toContain("@media (max-width: 1279px)");
    expect(mobile).toContain("@media (max-width: 799px)");
    expect(mobile).toContain("grid-template-rows: auto minmax(0, 1fr) auto");
  });

  it("keeps conversation panes bounded inside the application viewport",()=>{
    expect(responsive).toContain(".content.sessionContent");
    expect(responsive).toContain("touch-action: pan-y");
    expect(responsive).toContain(".sessionTabPanel .transcriptScroll");
    expect(responsive).toContain("max-height: none");
  });

  it("uses vertical mobile discovery instead of horizontal application carousels",()=>{
    expect(mobile).toContain(".mobileSessionTrigger");
    expect(mobile).toContain(".mobileSessionDialog");
    expect(mobile).toContain(".sessionItems");
    expect(mobile).not.toContain("touch-action: pan-x");
    expect(mobile).not.toContain("scroll-snap-type: x");
  });

  it("lays out mobile metrics and workspace tabs without sideways scrolling",()=>{
    expect(mobile).toContain("grid-template-columns: repeat(3, minmax(0, 1fr))");
    expect(mobile).toContain("grid-template-columns: repeat(2, minmax(0, 1fr))");
    expect(mobile).toContain("flex-wrap: wrap");
  });

  it("keeps narrow-tablet workspace tabs inside the content column",()=>{
    expect(responsive).toMatch(/@media \(max-width: 1279px\)[\s\S]*?\.sessionTabs \{[\s\S]*?grid-template-columns: repeat\(2, minmax\(0, 1fr\)\)/);
  });

  it("provides a viewport-sized expanded workspace and compact jump controls",()=>{
    expect(responsive).toContain(".sessionWorkspace.expanded");
    expect(responsive).toContain(".jumpControl");
    expect(timeline).toContain(".timelineJourneyLayout");
    expect(timeline).toContain(".timelineNavigator");
  });

  it("keeps mobile turn details above navigation with their own full scroll range",()=>{
    expect(mobile).toMatch(/\.detailBackdrop \{[\s\S]*?z-index: 80/);
    expect(mobile).toMatch(/\.turnDetail \{[\s\S]*?max-height: calc\(100dvh/);
    expect(mobile).toContain("overscroll-behavior-y: contain");
    expect(mobile).toContain("-webkit-overflow-scrolling: touch");
  });

  it("uses readable prompt checkpoints in normal and expanded timelines",()=>{
    expect(timeline).toContain(".journeyCheckpoint");
    expect(timeline).toContain(".journeyPrompt");
    expect(timeline).toContain(".timelineCard.expandedTimeline");
    expect(timeline).toContain("grid-template-columns:minmax(180px,240px) minmax(0,1fr)");
    expect(mobile).toMatch(/\.timelineNavigator \{[\s\S]*?display: none/);
  });
});
