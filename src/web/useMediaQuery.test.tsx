/** @module UseMediaQueryTest @description Anchors media-query tracking with and without matchMedia support. */
import {act,cleanup,renderHook} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {useMediaQuery} from "./useMediaQuery";

afterEach(()=>{cleanup();vi.unstubAllGlobals()});

describe("media query hook",()=>{
  it("follows changes and stops listening on unmount",()=>{
    let listener=()=>{};
    const list={matches:false,addEventListener:vi.fn((_:string,callback:()=>void)=>{listener=callback}),removeEventListener:vi.fn()};
    vi.stubGlobal("matchMedia",vi.fn(()=>list));
    const {result,unmount}=renderHook(()=>useMediaQuery("(max-width: 800px)"));
    expect(result.current).toBe(false);
    list.matches=true;
    act(()=>listener());
    expect(result.current).toBe(true);
    unmount();
    expect(list.removeEventListener).toHaveBeenCalled();
  });

  it("reports false where matchMedia is missing",()=>{
    vi.stubGlobal("matchMedia",undefined);
    expect(renderHook(()=>useMediaQuery("(max-width: 800px)")).result.current).toBe(false);
  });
});
