/** @module ReviewStateTest @description Anchors how append-only review records become the session's current decision and how notes name their target. */
import {describe,expect,it} from "vitest";
import {currentDecision,decisionKind,decisionLabel,newestFirst,noteTarget} from "./reviewState";
import type {Review} from "./types";

const review=(status:string,at:string):Review=>({id:at,session_id:"s",reviewer:"local owner",status,summary:"s",created_at:`2026-01-0${at}T00:00:00Z`});

describe("review state",()=>{
  it("maps owner, hosted and legacy statuses to one vocabulary and keeps unknown text",()=>{
    expect(decisionKind("reviewed")).toBe("reviewed");
    expect(decisionKind(" Approved ")).toBe("reviewed");
    expect(decisionKind("changes_requested")).toBe("followup");
    expect(decisionKind("needs_follow_up")).toBe("followup");
    expect(decisionKind("commented")).toBe("commented");
    expect(decisionKind("blocked")).toBe("other");
    expect(decisionLabel("changes_requested")).toBe("Needs follow-up");
    expect(decisionLabel("reviewed")).toBe("Reviewed");
    expect(decisionLabel("commented")).toBe("Commented");
    expect(decisionLabel("blocked")).toBe("blocked");
  });

  it("treats the newest decision as current and no decision as ready for review",()=>{
    expect(currentDecision([])).toEqual({kind:"pending",label:"Ready for review"});
    const decision=currentDecision([review("reviewed","1"),review("changes_requested","3"),review("approved","2")]);
    expect(decision).toMatchObject({kind:"followup",label:"Needs follow-up"});
    expect(decision.review?.id).toBe("3");
    expect(currentDecision([review("blocked","1")])).toMatchObject({kind:"other",label:"blocked"});
    expect(currentDecision([review("commented","1")])).toMatchObject({kind:"commented",label:"Commented"});
    const history=[review("a","1"),review("b","2")];
    expect(newestFirst(history).map(value=>value.id)).toEqual(["2","1"]);
    currentDecision(history);
    // Both leave the caller's records in their original order.
    expect(history.map(value=>value.id)).toEqual(["1","2"]);
  });

  it("names a note's target by prompt number, the whole session, or a prompt that no longer exists",()=>{
    const turns=[{id:"t1",sequence:1},{id:"t2",sequence:2}];
    expect(noteTarget({},turns)).toBe("Entire session");
    expect(noteTarget({turn_id:"t2"},turns)).toBe("Prompt 2");
    expect(noteTarget({turn_id:"gone"},turns)).toBe("A prompt no longer in this session");
  });
});
