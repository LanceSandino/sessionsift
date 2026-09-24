/** @module SupportComponentsTest @description Covers the support-code card and the bridge connection card. */
import "@testing-library/jest-dom/vitest";
import React from "react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {BridgeConnection} from "./BridgeConnection";
import {SupportShare} from "./SupportShare";

const json=(value:unknown,status=200)=>Promise.resolve(new Response(JSON.stringify(value),{status}));
const future=(seconds:number)=>new Date(Date.now()+seconds*1000).toISOString();

afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()});

describe("SupportShare",()=>{
  it("explains how to connect when no bridge is configured",async()=>{
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>String(input).endsWith("/bridge")?json({connected:false,device_id:"device_1"}):json([])));
    render(<SupportShare sessionId="s1" sessionTitle="Checkout bug"/>);
    expect(await screen.findByText(/to generate support codes/)).toBeInTheDocument();
    expect(screen.queryByRole("button",{name:"Generate support code"})).not.toBeInTheDocument();
  });

  it("generates a code, shows it once with a countdown, then reports the supporter and disconnects",async()=>{
    let supports:unknown[]=[];
    const waiting={id:"support_1",session_id:"s1",session_title:"Checkout bug",role:"comment",operations:[],created_at:new Date().toISOString(),code_expires_at:future(572),access_minutes:240,failed_attempts:0,status:"waiting"};
    const fetchMock=vi.fn((input:RequestInfo|URL,init?:RequestInit)=>{
      const path=String(input);
      if(path.endsWith("/bridge"))return json({connected:true,url:"https://bridge.example",device_id:"device_1"});
      if(path.endsWith("/support")&&init?.method==="POST"){supports=[waiting];return json({support:waiting,code:"ABCD-EFGH-JKMN",connect_url:"https://bridge.example/support"},201)}
      if(path.endsWith("/support/end")){supports=[{...waiting,status:"ended",end_reason:"ended_by_owner"}];return json(supports[0])}
      return json(supports);
    });
    vi.stubGlobal("fetch",fetchMock);
    render(<SupportShare sessionId="s1" sessionTitle="Checkout bug"/>);
    expect(await screen.findByText("https://bridge.example/support")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Support access"),{target:{value:"comment"}});
    fireEvent.change(screen.getByLabelText("Support duration"),{target:{value:"240"}});
    fireEvent.click(screen.getByRole("button",{name:"Generate support code"}));
    expect(await screen.findByLabelText("Support code")).toHaveTextContent("ABCD-EFGH-JKMN");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/remote/support",expect.objectContaining({method:"POST",body:JSON.stringify({session_id:"s1",role:"comment",minutes:240})}));
    expect(screen.getByText(/Expires in 9:3\d/)).toBeInTheDocument();
    expect(screen.getByText(/Waiting for your supporter/)).toBeInTheDocument();

    supports=[{...waiting,status:"connected",supporter:"helper@example.com",connected_at:new Date().toISOString(),expires_at:future(3600),last_activity_at:new Date().toISOString()}];
    expect(await screen.findByText("Connected: helper@example.com",{},{timeout:5000})).toBeInTheDocument();
    expect(screen.queryByLabelText("Support code")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Disconnect supporter"}));
    await waitFor(()=>expect(fetchMock).toHaveBeenCalledWith("/api/v1/remote/support/end",expect.objectContaining({method:"POST"})));
    expect(await screen.findByRole("button",{name:"Generate support code"})).toBeInTheDocument();
  });

  it("does not redisplay a code it did not create in this view and surfaces errors",async()=>{
    const waiting={id:"support_2",session_id:"s1",role:"view",operations:[],created_at:new Date().toISOString(),code_expires_at:future(300),access_minutes:60,failed_attempts:0,status:"waiting"};
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL,init?:RequestInit)=>{const path=String(input);if(path.endsWith("/bridge"))return json({connected:true,url:"https://bridge.example",device_id:"d"});if(path.endsWith("/support/end")&&init?.method==="POST")return json({error:"bridge unreachable"},400);return json([waiting])}));
    render(<SupportShare sessionId="s1" sessionTitle="Checkout bug"/>);
    expect(await screen.findByText(/The code was shown when it was created/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Cancel code"}));
    expect(await screen.findByText("bridge unreachable")).toBeInTheDocument();
  });
});

describe("BridgeConnection",()=>{
  it("connects the device, shows a link code, and adopts the owner once linked",async()=>{
    let bridge:{connected:boolean;device_id:string;url?:string;owner?:string;name?:string}={connected:false,device_id:"device_1"};
    const fetchMock=vi.fn((input:RequestInfo|URL,init?:RequestInit)=>{
      const path=String(input);
      if(path.endsWith("/bridge/connect")){bridge={connected:true,device_id:"device_1",url:"https://bridge.example",name:"Laptop"};return json(bridge)}
      if(path.endsWith("/bridge/link"))return json({code:"LINK-CODE-0000",expires_at:future(600),link_url:"https://bridge.example/"},201);
      if(path.endsWith("/bridge/refresh")){bridge={...bridge,owner:"owner@example.com"};return json(bridge)}
      if(path.endsWith("/bridge"))return json(bridge);
      if(path.endsWith("/support/end")&&init?.method==="POST")return json({});
      return json([{id:"support_9",session_id:"s1",session_title:"Checkout bug",role:"view",operations:[],created_at:new Date().toISOString(),code_expires_at:future(200),access_minutes:60,failed_attempts:0,status:"waiting"}]);
    });
    vi.stubGlobal("fetch",fetchMock);
    render(<BridgeConnection/>);
    expect(await screen.findByText("Not connected")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Bridge URL"),{target:{value:"https://bridge.example"}});
    fireEvent.change(screen.getByLabelText("Device name"),{target:{value:"Laptop"}});
    fireEvent.click(screen.getByRole("button",{name:"Connect this device"}));
    expect(await screen.findByText("https://bridge.example")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/remote/bridge/connect",expect.objectContaining({body:JSON.stringify({url:"https://bridge.example",name:"Laptop"})}));
    expect(screen.getByText("Checkout bug")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Link to account"}));
    expect(await screen.findByLabelText("Device link code")).toHaveTextContent("LINK-CODE-0000");
    expect(await screen.findByText("owner@example.com",{},{timeout:6000})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Cancel"}));
    await waitFor(()=>expect(fetchMock).toHaveBeenCalledWith("/api/v1/remote/support/end",expect.objectContaining({method:"POST"})));
  });

  it("reports connection failures",async()=>{
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>String(input).endsWith("/bridge/connect")?json({error:"bridge URL must be HTTPS"},400):String(input).endsWith("/bridge")?json({connected:false,device_id:"d"}):json([])));
    render(<BridgeConnection/>);
    fireEvent.change(await screen.findByLabelText("Bridge URL"),{target:{value:"http://insecure.example"}});
    fireEvent.click(screen.getByRole("button",{name:"Connect this device"}));
    expect(await screen.findByText("bridge URL must be HTTPS")).toBeInTheDocument();
  });
});
