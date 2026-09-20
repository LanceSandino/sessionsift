/** @module UseLatestScrollTest @description Verifies latest-first scroll initialization and checkpoint navigation. */
import {render} from "@testing-library/react";
import {createRef} from "react";
import {describe,expect,it,vi} from "vitest";
import {scrollToCheckpoint,useLatestScroll} from "./useLatestScroll";

function Fixture(){const ref=createRef<HTMLDivElement>();useLatestScroll(ref,"session-a");return <div ref={ref} data-testid="viewport"/>}

describe("latest scroll",()=>{
  it("starts a viewport at its latest content",()=>{Object.defineProperty(HTMLElement.prototype,"scrollHeight",{configurable:true,value:640});const{getByTestId}=render(<Fixture/>);expect(getByTestId("viewport").scrollTop).toBe(640)});
  it("moves to a named prompt checkpoint",()=>{const scrollIntoView=vi.fn();render(<div id="prompt-2" ref={node=>{if(node)node.scrollIntoView=scrollIntoView}}/>);scrollToCheckpoint("prompt-2");expect(scrollIntoView).toHaveBeenCalledWith({behavior:"smooth",block:"start"})});
});
