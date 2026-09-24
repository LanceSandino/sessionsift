/** @module ThemeTest @description Anchors theme resolution and storage failure tolerance. */
import {afterEach,describe,expect,it,vi} from "vitest";
import {applyTheme,initialTheme,nextTheme,readStoredTheme,storeTheme,systemPrefersDark} from "./theme";

afterEach(()=>{vi.restoreAllMocks();vi.unstubAllGlobals();window.localStorage.clear()});

describe("theme",()=>{
  it("prefers a stored choice, then the operating system, and ignores unknown values",()=>{
    expect(initialTheme("dark",false)).toBe("dark");
    expect(initialTheme("light",true)).toBe("light");
    expect(initialTheme("sepia",true)).toBe("dark");
    expect(initialTheme(null,false)).toBe("light");
    expect(nextTheme("dark")).toBe("light");
    expect(nextTheme("light")).toBe("dark");
  });

  it("stores and applies the choice, and survives storage that throws",()=>{
    storeTheme("dark");
    expect(readStoredTheme()).toBe("dark");
    applyTheme("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    vi.spyOn(Storage.prototype,"getItem").mockImplementation(()=>{throw new Error("blocked")});
    vi.spyOn(Storage.prototype,"setItem").mockImplementation(()=>{throw new Error("blocked")});
    expect(readStoredTheme()).toBeNull();
    expect(()=>storeTheme("light")).not.toThrow();
  });

  it("reads the system preference only where matchMedia exists",()=>{
    vi.stubGlobal("matchMedia",undefined);
    expect(systemPrefersDark()).toBe(false);
    vi.stubGlobal("matchMedia",(query:string)=>({matches:query.includes("dark")}));
    expect(systemPrefersDark()).toBe(true);
  });
});
