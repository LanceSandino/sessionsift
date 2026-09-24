import {describe,expect,it} from "vitest";
import {activeSupportFor,countdown,endReasonLabel,liveStatus,supportHeadline} from "./supportState";
import {sessionPlace,sourceCounts,sourceInfo,sourceOf} from "./sources";
import type {SupportSession} from "./types";

const base=Date.parse("2026-09-23T12:00:00Z");
const iso=(offsetSeconds:number)=>new Date(base+offsetSeconds*1000).toISOString();
const support=(overrides:Partial<SupportSession>={}):SupportSession=>({id:"support_1",session_id:"s1",role:"view",operations:[],created_at:iso(0),code_expires_at:iso(600),access_minutes:30,failed_attempts:0,status:"waiting",...overrides});

describe("countdown",()=>{
  it("formats minutes, hours and expiry",()=>{
    expect(countdown(iso(572),base)).toBe("9:32");
    expect(countdown(iso(5),base)).toBe("0:05");
    expect(countdown(iso(3600+65),base)).toBe("1:01:05");
    expect(countdown(iso(0),base)).toBe("0:00");
    expect(countdown(iso(-10),base)).toBe("0:00");
    expect(countdown(undefined,base)).toBe("0:00");
    expect(countdown("not a date",base)).toBe("0:00");
  });
});

describe("support lifecycle",()=>{
  it("expires waiting codes and connected access locally",()=>{
    expect(liveStatus(support(),base)).toBe("waiting");
    expect(liveStatus(support(),base+600_000)).toBe("expired");
    const connected=support({status:"connected",connected_at:iso(10),expires_at:iso(1800),supporter:"helper@example.com"});
    expect(liveStatus(connected,base+1799_000)).toBe("connected");
    expect(liveStatus(connected,base+1800_000)).toBe("expired");
    expect(liveStatus(support({status:"ended"}),base)).toBe("ended");
    expect(liveStatus(support({status:"expired"}),base)).toBe("expired");
    expect(liveStatus(support({status:"connected"}),base)).toBe("connected");
  });
  it("describes each state for the owner",()=>{
    expect(supportHeadline(support(),base)).toMatch(/Waiting/);
    expect(supportHeadline(support({status:"connected",supporter:"helper@example.com",expires_at:iso(60)}),base)).toBe("Connected: helper@example.com");
    expect(supportHeadline(support({status:"connected",expires_at:iso(60)}),base)).toBe("Connected: supporter");
    expect(supportHeadline(support({status:"expired",connected_at:iso(1)}),base)).toBe("Support access has expired");
    expect(supportHeadline(support({status:"expired"}),base)).toBe("The code expired before it was used");
    expect(supportHeadline(support({status:"ended",end_reason:"supporter_disconnected"}),base)).toBe("Your supporter disconnected");
    expect(endReasonLabel("too_many_attempts")).toMatch(/wrong codes/);
    expect(endReasonLabel("bridge_unavailable")).toMatch(/bridge/);
    expect(endReasonLabel(undefined)).toBe("You ended this support session");
  });
  it("selects the newest live support session for a conversation",()=>{
    const older=support({id:"a",created_at:iso(0)});
    const newer=support({id:"b",created_at:iso(30)});
    const other=support({id:"c",session_id:"s2",created_at:iso(60)});
    const ended=support({id:"d",created_at:iso(90),status:"ended"});
    expect(activeSupportFor([older,newer,other,ended],"s1",base)?.id).toBe("b");
    expect(activeSupportFor([older],"s1",base+601_000)).toBeUndefined();
    expect(activeSupportFor([other],"s1",base)).toBeUndefined();
    const connected=support({id:"e",created_at:iso(1),status:"connected",expires_at:iso(900)});
    expect(activeSupportFor([connected],"s1",base+700_000)?.id).toBe("e");
  });
});

describe("sources",()=>{
  it("labels every provider and defaults legacy rows to Codex",()=>{
    expect(sourceOf(undefined)).toBe("codex");
    expect(sourceOf({source:undefined})).toBe("codex");
    expect(sourceOf({source:"claude_code"})).toBe("claude_code");
    expect(sourceOf({source:"unknown" as never})).toBe("codex");
    expect(sourceInfo({source:"claude"}).label).toBe("Claude");
    expect(sourceInfo({source:"claude_code"}).assistant).toBe("Claude");
    expect(sourceInfo({source:"chatgpt"}).chat).toBe(true);
  });
  it("places chats by product and developer sessions by repository",()=>{
    expect(sessionPlace({source:"chatgpt",repo_display_name:"ignored"})).toBe("ChatGPT");
    expect(sessionPlace({source:"claude"})).toBe("Claude");
    expect(sessionPlace({source:"claude_code",repo_display_name:"api"})).toBe("api");
    expect(sessionPlace({source:"claude_code"})).toBe("Claude Code");
    expect(sessionPlace({source:undefined})).toBe("Codex");
  });
  it("counts visible sessions per provider",()=>{
    expect(sourceCounts([{source:"codex"},{source:undefined},{source:"claude_code"},{source:"claude"},{source:"chatgpt"},{source:"claude_code",session_kind:"internal_review"}])).toEqual({codex:2,claude_code:1,chatgpt:1,claude:1});
  });
});
