/** @module SessionFiltersTest @description Mutation-resistant tests for conversation filtering behavior. */
import {describe,expect,it} from "vitest";
import type {Session} from "../types";
import {filterSessions,type SessionFilters} from "./sessionFilters";

const baseFilters:SessionFilters={query:"",source:"",repo:"",model:"",date:"",highTokens:false,failures:false,findings:false,internal:false};
const session=(overrides:Partial<Session>={}):Session=>({
  id:"session-1",
  title:"Responsive workspace",
  repo_display_name:"sessionsift",
  model:"gpt-6",
  started_at:"2026-09-20T12:00:00Z",
  ended_at:"2026-09-20T12:01:00Z",
  status:"completed",
  archived:false,
  turns:[],tool_calls:[],findings:[],events:[],
  metrics:{duration_ms:60000,elapsed_duration_ms:60000,usage_delta_percent:0,usage_measured_turns:0,turn_count:1,input_tokens:100,cached_input_tokens:0,output_tokens:10,reasoning_tokens:0,cache_ratio:0,tool_calls:0,builtin_tool_calls:0,mcp_calls:0,app_tool_calls:0,unique_tools:0,unique_mcp_servers:0,failed_tool_calls:0,reads:0,searches:0,edits:0,validations:0,compactions:0,exploration_ratio:0,finding_count:0},
  ...overrides,
});
const ids=(sessions:Session[])=>sessions.map(item=>item.id);

describe("filterSessions",()=>{
  it("keeps ordinary sessions and hides internal reviews by default",()=>{
    expect(ids(filterSessions([session(),session({id:"review",session_kind:"internal_review"})],baseFilters))).toEqual(["session-1"]);
    expect(ids(filterSessions([session({id:"review",session_kind:"internal_review"})],{...baseFilters,internal:true}))).toEqual(["review"]);
  });

  it("treats a missing source as Codex and filters explicit ChatGPT sessions",()=>{
    const codex=session();
    const chat=session({id:"chat",source:"chatgpt"});
    expect(ids(filterSessions([codex,chat],{...baseFilters,source:"codex"}))).toEqual(["session-1"]);
    expect(ids(filterSessions([codex,chat],{...baseFilters,source:"chatgpt"}))).toEqual(["chat"]);
  });

  it("searches title and repository case-insensitively and trims input",()=>{
    const other=session({id:"other",title:"API review",repo_display_name:"backend"});
    expect(ids(filterSessions([session(),other],{...baseFilters,query:"  RESPONSIVE  "}))).toEqual(["session-1"]);
    expect(ids(filterSessions([session(),other],{...baseFilters,query:"BACKEND"}))).toEqual(["other"]);
    expect(filterSessions([session({repo_display_name:undefined})],{...baseFilters,query:"missing"})).toEqual([]);
    expect(filterSessions([session({repo_display_name:undefined})],{...baseFilters,query:"stryker was here"})).toEqual([]);
  });

  it.each([
    ["repo",{repo:"sessionsift"},{repo_display_name:"elsewhere"}],
    ["model",{model:"gpt-6"},{model:"gpt-5"}],
    ["date",{date:"2026-09-20"},{started_at:"2026-09-21T00:00:00Z"}],
  ] as const)("applies the %s selector",(_name,filter,nonMatch)=>{
    expect(ids(filterSessions([session(),session({id:"other",...nonMatch})],{...baseFilters,...filter}))).toEqual(["session-1"]);
  });

  it("applies token, failure, and finding thresholds at their boundaries",()=>{
    const baseline=session().metrics!;
    const qualifying=session({id:"qualifying",metrics:{...baseline,input_tokens:100000,failed_tool_calls:1,finding_count:1}});
    const below=session({id:"below",metrics:{...baseline,input_tokens:99999,failed_tool_calls:0,finding_count:0}});
    expect(ids(filterSessions([qualifying,below],{...baseFilters,highTokens:true}))).toEqual(["qualifying"]);
    expect(ids(filterSessions([qualifying,below],{...baseFilters,failures:true}))).toEqual(["qualifying"]);
    expect(ids(filterSessions([qualifying,below],{...baseFilters,findings:true}))).toEqual(["qualifying"]);
  });

  it("does not match metric filters when metrics are absent",()=>{
    const missing=session({metrics:undefined});
    expect(filterSessions([missing],{...baseFilters,highTokens:true})).toEqual([]);
    expect(filterSessions([missing],{...baseFilters,failures:true})).toEqual([]);
    expect(filterSessions([missing],{...baseFilters,findings:true})).toEqual([]);
  });
});
