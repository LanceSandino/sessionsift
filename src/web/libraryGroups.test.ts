/** @module LibraryGroupsTest @description Anchors recency grouping of the session library at its day boundaries and for each sort order. */
import {describe,expect,it} from "vitest";
import {groupByRecency,libraryGroups,libraryTime,recencyLabel} from "./libraryGroups";
import type {Session} from "./types";

const now=new Date(2026,8,24,15,0,0).getTime();
const at=(days:number,hours=12)=>new Date(2026,8,24-days,hours,0,0).toISOString();
const session=(id:string,ended:string):Session=>({id,title:id,started_at:ended,ended_at:ended,status:"completed",archived:false});

describe("library groups",()=>{
  it("labels today, yesterday, the rest of the week and earlier by local day",()=>{
    expect(recencyLabel(at(0,0),now)).toBe("Today");
    expect(recencyLabel(at(1,23),now)).toBe("Yesterday");
    expect(recencyLabel(at(1,0),now)).toBe("Yesterday");
    expect(recencyLabel(at(2),now)).toBe("This week");
    expect(recencyLabel(at(6,0),now)).toBe("This week");
    expect(recencyLabel(at(7,23),now)).toBe("Earlier");
    expect(recencyLabel("not a date",now)).toBe("Undated");
  });

  it("keeps library order inside groups and falls back to the start time",()=>{
    const groups=groupByRecency([session("a",at(0)),session("b",at(9)),session("c",at(0)),{...session("d",at(1)),ended_at:""}],now);
    expect(groups.map(group=>[group.label,group.sessions.map(value=>value.id)])).toEqual([["Today",["a","c"]],["Earlier",["b"]],["Yesterday",["d"]]]);
  });

  // Regression anchor for CU-20260924-008: the library groups by the time it sorts on,
  // so a conversation created long ago but updated today is listed under Today.
  it("groups date orders by the time they sort on and ranks title and token orders as one list",()=>{
    const revived={...session("revived",at(0)),started_at:at(10)};
    const fresh={...session("fresh",at(1)),metrics:{input_tokens:900} as Session["metrics"]};
    const old={...session("old",at(9)),title:"alpha"};
    const shape=(order:Parameters<typeof libraryGroups>[1])=>libraryGroups([old,fresh,revived],order,now).map(group=>[group.label,group.sessions.map(value=>value.id)]);
    expect(shape("updated_desc")).toEqual([["Today",["revived"]],["Yesterday",["fresh"]],["Earlier",["old"]]]);
    expect(shape("started_desc")).toEqual([["Yesterday",["fresh"]],["Earlier",["old","revived"]]]);
    expect(shape("started_asc")).toEqual([["Earlier",["revived","old"]],["Yesterday",["fresh"]]]);
    expect(shape("title_asc")).toEqual([["Title A–Z",["old","fresh","revived"]]]);
    expect(shape("tokens_desc")).toEqual([["Most input tokens",["fresh","old","revived"]]]);
    expect(libraryGroups([],"title_asc",now)).toEqual([]);
    expect(libraryTime(revived,"started_asc")).toBe(revived.started_at);
    expect(libraryTime(revived,"updated_asc")).toBe(revived.ended_at);
    expect(libraryTime({...revived,ended_at:""},"updated_desc")).toBe(revived.started_at);
  });
});
