/** @module UseCollaborationTest @description Anchors loading and recording of owner notes and decisions, including legacy null collections. */
import {act,cleanup,renderHook,waitFor} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {ownerAuthor,useCollaboration} from "./useCollaboration";

afterEach(()=>{cleanup();vi.unstubAllGlobals()});
const ok=(value:unknown)=>Promise.resolve(new Response(JSON.stringify(value),{status:200}));

describe("collaboration store",()=>{
  it("treats legacy null collections as empty and reloads after each write",async()=>{
    let annotations:unknown=null;
    const fetchMock=vi.fn((input:RequestInfo|URL,init?:RequestInit)=>{const path=String(input);if(init?.method){annotations=[{id:"n",session_id:"s",author:ownerAuthor,body:"b",created_at:"2026-01-01T00:00:00Z"}];return ok({})}return path.includes("annotations")?ok(annotations):ok(null)});
    vi.stubGlobal("fetch",fetchMock);
    const {result}=renderHook(()=>useCollaboration("s"));
    await waitFor(()=>expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(result.current.annotations).toEqual([]);
    expect(result.current.reviews).toEqual([]);
    await act(()=>result.current.addNote("b","t1"));
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/remote/annotations",expect.objectContaining({body:JSON.stringify({session_id:"s",turn_id:"t1",author:ownerAuthor,body:"b"})}));
    expect(result.current.annotations).toHaveLength(1);
    await act(()=>result.current.recordDecision("reviewed","ok"));
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/remote/reviews",expect.objectContaining({body:JSON.stringify({session_id:"s",reviewer:ownerAuthor,status:"reviewed",summary:"ok"})}));
  });

  it("reports load and write failures and does nothing without a session",async()=>{
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL,init?:RequestInit)=>init?.method?Promise.resolve(new Response(JSON.stringify({error:"denied"}),{status:403})):Promise.reject(new Error("offline"))));
    const {result,rerender}=renderHook(({id}:{id?:string})=>useCollaboration(id),{initialProps:{id:"s" as string|undefined}});
    await waitFor(()=>expect(result.current.error).toBe("offline"));
    await act(async()=>{await expect(result.current.addNote("b")).rejects.toThrow("denied")});
    expect(result.current.error).toBe("denied");
    await act(async()=>{await expect(result.current.recordDecision("reviewed","s")).rejects.toThrow("denied")});
    rerender({id:undefined});
    expect(result.current.annotations).toEqual([]);
    await act(()=>result.current.addNote("b"));
    await act(()=>result.current.recordDecision("reviewed","s"));
  });
});
