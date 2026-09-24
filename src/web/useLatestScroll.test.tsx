/** @module UseLatestScrollTest @description Verifies latest-first scroll initialization and checkpoint navigation. */
import {render,fireEvent,cleanup} from "@testing-library/react";
import {createRef} from "react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {scrollToCheckpoint,useLatestScroll} from "./useLatestScroll";

function Fixture(){const ref=createRef<HTMLDivElement>();useLatestScroll(ref,"session-a");return <div ref={ref} data-testid="viewport"/>}
afterEach(()=>{cleanup();vi.unstubAllGlobals()});

describe("latest scroll",()=>{
  it("preserves latest on resize without overriding a reader scrolling into history",()=>{
    let resize=()=>{};const disconnect=vi.fn();vi.stubGlobal("ResizeObserver",class{constructor(callback:()=>void){resize=callback}observe(){}disconnect(){disconnect()}});
    const {getByTestId,unmount}=render(<Fixture/>);const viewport=getByTestId("viewport");Object.defineProperties(viewport,{clientWidth:{configurable:true,value:390},clientHeight:{configurable:true,value:400},scrollHeight:{configurable:true,value:1000}});
    resize();expect(viewport.scrollTop).toBe(1000);
    viewport.scrollTop=100;fireEvent.scroll(viewport);Object.defineProperty(viewport,"clientWidth",{configurable:true,value:768});resize();expect(viewport.scrollTop).toBe(100);
    viewport.scrollTop=600;fireEvent.scroll(viewport);Object.defineProperty(viewport,"scrollHeight",{configurable:true,value:1500});resize();expect(viewport.scrollTop).toBe(1500);unmount();expect(disconnect).toHaveBeenCalled();
  });
  it("starts a viewport at its latest content",()=>{Object.defineProperty(HTMLElement.prototype,"scrollHeight",{configurable:true,value:640});const{getByTestId}=render(<Fixture/>);expect(getByTestId("viewport").scrollTop).toBe(640)});
  it("reopens a revisited view where the reader left it and opens other views at the top when asked",()=>{
    Object.defineProperty(HTMLElement.prototype,"scrollHeight",{configurable:true,value:900});
    const memory=new Map<string,number>();
    function Remembered({view,start}:{view:string;start?:"latest"|"top"}){const ref=createRef<HTMLDivElement>();useLatestScroll(ref,view,{memory,start});return <div ref={ref} data-testid="viewport"/>}
    const {getByTestId,rerender}=render(<Remembered view="conversation"/>);
    expect(getByTestId("viewport").scrollTop).toBe(900);
    getByTestId("viewport").scrollTop=120;fireEvent.scroll(getByTestId("viewport"));
    expect(memory.get("conversation")).toBe(120);
    rerender(<Remembered view="tools" start="top"/>);
    expect(getByTestId("viewport").scrollTop).toBe(0);
    rerender(<Remembered view="conversation"/>);
    expect(getByTestId("viewport").scrollTop).toBe(120);
  });
  it("moves to a named prompt checkpoint",()=>{const scrollIntoView=vi.fn();render(<div id="prompt-2" ref={node=>{if(node)node.scrollIntoView=scrollIntoView}}/>);scrollToCheckpoint("prompt-2");expect(scrollIntoView).toHaveBeenCalledWith({behavior:"smooth",block:"start"})});
});
