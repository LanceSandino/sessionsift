/** @module NavigationTest @description Anchors deep links for workspace pages and session views, including legacy connection paths. */
import {afterEach,describe,expect,it} from "vitest";
import {pushRoute,readRoute,replaceRoute,routePath} from "./navigation";

afterEach(()=>window.history.replaceState({},"","/"));

describe("navigation",()=>{
  it("reads sessions, session views, pages and legacy connection paths",()=>{
    expect(readRoute("/")).toEqual({page:"sessions"});
    expect(readRoute("/sessions/claude%3Aabc")).toEqual({page:"sessions",sessionId:"claude:abc"});
    expect(readRoute("/sessions/abc/timeline")).toEqual({page:"sessions",sessionId:"abc",view:"timeline"});
    expect(readRoute("/sessions/abc/conversation")).toEqual({page:"sessions",sessionId:"abc"});
    expect(readRoute("/sessions/abc/unknown")).toEqual({page:"sessions",sessionId:"abc"});
    expect(readRoute("/analytics")).toEqual({page:"analytics"});
    expect(readRoute("/compare")).toEqual({page:"compare"});
    expect(readRoute("/optimize")).toEqual({page:"optimize"});
    expect(readRoute("/connections")).toEqual({page:"connections"});
    expect(readRoute("/remote-access")).toEqual({page:"connections"});
    expect(readRoute("/remote-review")).toEqual({page:"connections"});
    expect(readRoute("/diagnostics")).toEqual({page:"diagnostics"});
    expect(readRoute("/changelog")).toEqual({page:"changelog"});
    expect(readRoute("/nope")).toEqual({page:"sessions"});
  });

  it("writes canonical paths and only touches history when the path changes",()=>{
    expect(routePath({page:"sessions"})).toBe("/");
    expect(routePath({page:"sessions",sessionId:"claude:a b"})).toBe("/sessions/claude%3Aa%20b");
    expect(routePath({page:"sessions",sessionId:"a",view:"conversation"})).toBe("/sessions/a");
    expect(routePath({page:"sessions",sessionId:"a",view:"review"})).toBe("/sessions/a/review");
    expect(routePath({page:"connections"})).toBe("/connections");
    expect(routePath({page:"optimize"})).toBe("/optimize");
    expect(routePath({page:"changelog"})).toBe("/changelog");
    const length=window.history.length;
    pushRoute({page:"analytics"});
    expect(window.location.pathname).toBe("/analytics");
    pushRoute({page:"analytics"});
    expect(window.history.length).toBe(length+1);
    replaceRoute({page:"sessions",sessionId:"a",view:"tools"});
    expect(window.location.pathname).toBe("/sessions/a/tools");
    expect(window.history.length).toBe(length+1);
  });
});
