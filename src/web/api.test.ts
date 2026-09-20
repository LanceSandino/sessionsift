/** @module ApiPaginationTests @description Prevents silently truncating the local session library. */
import {afterEach,expect,it,vi} from "vitest";
import {api} from "./api";
afterEach(()=>vi.unstubAllGlobals());
it("collects all library pages, including an exact full page",async()=>{
 const fetcher=vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(Array.from({length:100},(_,i)=>({id:String(i)}))))).mockResolvedValueOnce(new Response(JSON.stringify([{id:"last"}])));
 vi.stubGlobal("fetch",fetcher);
 const values=await api.sessions(true);
 expect(values).toHaveLength(101);expect(values[100].id).toBe("last");
 expect(fetcher).toHaveBeenNthCalledWith(2,"/api/v1/sessions?limit=100&cursor=100&include_internal=true",expect.anything());
});
