/** @module SessionRevisionTest @description Anchors detail-refresh gating for large session payloads. */
import {describe,expect,it} from "vitest";
import type {Session} from "./types";
import {sessionRevision} from "./sessionRevision";

const session={id:"s",title:"Session",started_at:"2026-09-20T10:00:00Z",ended_at:"2026-09-20T10:01:00Z",status:"completed",archived:false,metrics:{turn_count:1,tool_calls:2,input_tokens:3}} as Session;

describe("sessionRevision",()=>{
  it("stays stable for presentation-only summary refreshes",()=>{
    expect(sessionRevision({...session,title:"Renamed"})).toBe(sessionRevision(session));
  });

  it("changes when a completed prompt changes the detail",()=>{
    expect(sessionRevision({...session,ended_at:"2026-09-20T10:02:00Z"})).not.toBe(sessionRevision(session));
    expect(sessionRevision({...session,metrics:{...session.metrics!,turn_count:2}})).not.toBe(sessionRevision(session));
    expect(sessionRevision({...session,status:"active"})).not.toBe(sessionRevision(session));
  });

  it("does not refetch multi-megabyte detail for in-flight counters",()=>{
    expect(sessionRevision({...session,metrics:{...session.metrics!,tool_calls:4,input_tokens:99}})).toBe(sessionRevision(session));
  });
});
