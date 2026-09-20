/** @module TimelineWindowTest @description Locks latest-first windowing and scroll-position boundaries. */
import {describe,expect,it} from "vitest";
import {latestWindow,timelineScrollIndex} from "./timelineWindow";

describe("timeline window",()=>{
  it("keeps the newest bounded values and reports omitted history",()=>{
    expect(latestWindow([1,2,3,4,5],2)).toEqual({shown:[4,5],remaining:3});
    expect(latestWindow([1,2],20)).toEqual({shown:[1,2],remaining:0});
    expect(latestWindow([1,2],0)).toEqual({shown:[],remaining:2});
    expect(latestWindow([1,2],1.9)).toEqual({shown:[2],remaining:1});
  });

  it("maps scroll progress to a clamped evidence index",()=>{
    expect(timelineScrollIndex(0,600,200,5)).toBe(0);
    expect(timelineScrollIndex(200,600,200,5)).toBe(2);
    expect(timelineScrollIndex(400,600,200,5)).toBe(4);
    expect(timelineScrollIndex(-100,600,200,5)).toBe(0);
    expect(timelineScrollIndex(900,600,200,5)).toBe(4);
    expect(timelineScrollIndex(0,100,100,5)).toBe(4);
    expect(timelineScrollIndex(0,600,200,1)).toBe(0);
    expect(timelineScrollIndex(400,600,200,0)).toBe(0);
  });
});
