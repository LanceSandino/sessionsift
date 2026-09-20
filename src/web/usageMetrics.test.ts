/** @module UsageMetricsTest @description Anchors account-meter and model-transition presentation semantics. */
import {describe,expect,it} from "vitest";
import type {Turn} from "./types";
import {modelSettingPeriods,observedUsageDelta,usageDeltaLabel} from "./usageMetrics";

const turn=(sequence:number,model:string,effort:string,start?:number,end?:number):Turn=>({
  id:`t${sequence}`,sequence,started_at:`2026-09-20T10:0${sequence}:00Z`,ended_at:`2026-09-20T10:0${sequence}:30Z`,model,reasoning_effort:effort,
  rate_limit_used_start:start,rate_limit_used_end:end,input_tokens:0,cached_input_tokens:0,output_tokens:0,reasoning_tokens:0,
});

describe("usage metrics",()=>{
  it("reports only non-reset account-meter movement",()=>{
    expect(observedUsageDelta(turn(1,"sol","high",84,94))).toBe(10);
    expect(observedUsageDelta(turn(2,"sol","high",94,2))).toBeUndefined();
    expect(usageDeltaLabel(10)).toBe("+10 points");
  });

  it("collapses consecutive model settings while retaining transitions",()=>{
    const periods=modelSettingPeriods([
      turn(1,"gpt-5.6-sol","high",80,82),turn(2,"gpt-5.6-sol","high",82,84),turn(3,"gpt-6-astra","high",84,94),turn(4,"gpt-5.6-sol","high",94,95),
    ]);
    expect(periods).toHaveLength(3);
    expect(periods[0]).toMatchObject({firstSequence:1,lastSequence:2,usageDelta:4,measuredTurns:2});
    expect(periods[1]).toMatchObject({model:"gpt-6-astra",effort:"high",usageDelta:10});
  });
});
