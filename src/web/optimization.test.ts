/** @module OptimizationBehaviorTest @description Mutation anchors for signed deltas, lifecycle states, and follow-up payloads. */
import {describe,expect,it} from "vitest";
import type {OptimizationExperiment} from "./types";
import {formatOptimizationDelta,measurementUpdate,recommendationStates} from "./optimization";

describe("optimization behavior",()=>{
  it("formats positive, negative, zero, integer and decimal deltas without implying quality",()=>{
    expect(formatOptimizationDelta(2,"calls")).toBe("+2 calls");
    expect(formatOptimizationDelta(-2,"calls")).toBe("-2 calls");
    expect(formatOptimizationDelta(0,"calls")).toBe("0 calls");
    expect(formatOptimizationDelta(1.25,"points")).toBe("+1.25 points");
    expect(formatOptimizationDelta(-1.2,"points")).toBe("-1.2 points");
    expect(formatOptimizationDelta(1.05,"points")).toBe("+1.05 points");
    expect(formatOptimizationDelta(1.001,"points")).toBe("+1 points");
  });

  it("keeps every lifecycle state explicit and builds a bounded update",()=>{
    expect(recommendationStates).toEqual(["proposed","accepted","applied","dismissed","measured"]);
    const experiment:OptimizationExperiment={id:"exp",title:"Test",hypothesis:"Change",recommendation_id:"rec",baseline_session_id:"before",status:"running",created_at:"now",updated_at:"now"};
    expect(measurementUpdate(experiment,"after")).toEqual({id:"exp",title:"Test",hypothesis:"Change",recommendation_id:"rec",baseline_session_id:"before",followup_session_id:"after"});
  });
});
