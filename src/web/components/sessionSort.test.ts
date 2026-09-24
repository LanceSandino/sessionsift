/** @module SessionSortTest @description Anchors deterministic conversation ordering. */
import {describe,expect,it} from "vitest";
import type {Session} from "../types";
import {sortSessions} from "./sessionSort";

const session=(id:string,started_at:string,ended_at:string,input_tokens=0):Session=>({
  id,title:id,started_at,ended_at,status:"completed",archived:false,
  metrics:{duration_ms:0,elapsed_duration_ms:0,usage_delta_percent:0,usage_measured_turns:0,turn_count:1,input_tokens,cached_input_tokens:0,output_tokens:0,reasoning_tokens:0,cache_ratio:0,tool_calls:0,builtin_tool_calls:0,mcp_calls:0,app_tool_calls:0,unique_tools:0,unique_mcp_servers:0,failed_tool_calls:0,reads:0,searches:0,edits:0,validations:0,compactions:0,exploration_ratio:0,finding_count:0},
});

describe("sortSessions",()=>{
  const olderUpdated={...session("z-id","2026-09-22T12:00:00Z","2026-09-22T13:00:00Z",100),title:"Alpha"};
  const newestCreated={...session("a-id","2026-09-24T12:00:00Z","2026-09-24T12:05:00Z",1000),title:"Charlie"};
  const latestUpdated={...session("m-id","2026-09-20T12:00:00Z","2026-09-24T14:00:00Z",500),title:"Bravo"};

  it("defaults to most recently updated without mutating API order",()=>{
    const input=[olderUpdated,newestCreated,latestUpdated];
    expect(sortSessions(input,"updated_desc").map(item=>item.id)).toEqual(["m-id","a-id","z-id"]);
    expect(input.map(item=>item.id)).toEqual(["z-id","a-id","m-id"]);
  });

  it.each([
    ["updated_asc",["z-id","a-id","m-id"]],
    ["started_desc",["a-id","z-id","m-id"]],
    ["started_asc",["m-id","z-id","a-id"]],
    ["title_asc",["z-id","m-id","a-id"]],
    ["tokens_desc",["a-id","m-id","z-id"]],
  ] as const)("supports %s",(order,want)=>expect(sortSessions([olderUpdated,newestCreated,latestUpdated],order).map(item=>item.id)).toEqual(want));

  it("uses start time and stable title/id fallbacks when primary values tie",()=>{
    const early={...session("z","2026-09-20T10:00:00Z","2026-09-21T12:00:00Z"),title:"Same"};
    const late={...session("a","2026-09-20T11:00:00Z","2026-09-21T12:00:00Z"),title:"Same"};
    expect(sortSessions([early,late],"updated_desc").map(item=>item.id)).toEqual(["a","z"]);
    expect(sortSessions([late,early],"updated_asc").map(item=>item.id)).toEqual(["z","a"]);
    expect(sortSessions([early,late],"title_asc").map(item=>item.id)).toEqual(["a","z"]);
  });

  it("treats invalid dates and missing metrics as zero",()=>{
    const missing={...session("missing","invalid","invalid"),metrics:undefined,title:"Missing"};
    const measured={...session("measured","2026-09-20T00:00:00Z","2026-09-20T01:00:00Z",1),title:"Measured"};
    expect(sortSessions([missing,measured],"updated_desc").map(item=>item.id)).toEqual(["measured","missing"]);
    expect(sortSessions([measured,missing],"tokens_desc").map(item=>item.id)).toEqual(["measured","missing"]);
    expect(sortSessions([missing,measured],"tokens_desc").map(item=>item.id)).toEqual(["measured","missing"]);
  });
});
