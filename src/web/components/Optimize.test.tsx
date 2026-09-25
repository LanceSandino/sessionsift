/** @module OptimizeTest @description Covers the complete local recommendation, experiment, measurement, and report workflow. */
import "@testing-library/jest-dom/vitest";
import React from "react";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import type {OptimizationOverview} from "../types";
import {Optimize} from "./Optimize";
import {first,second,json} from "./testFixtures";

afterEach(()=>{cleanup();vi.unstubAllGlobals()});

const overview:OptimizationOverview={generated_at:"2026-09-25T12:00:00Z",period_start:"2026-09-18T12:00:00Z",summary:{sessions:2,turns:4,tool_calls:8,failed_tool_calls:1,findings:2,running_experiments:1,measured_experiments:1},recommendations:[{id:"tool-failures",title:"Shorten failure loops",summary:"Inspect failures before retrying.",confidence:"derived",evidence:["3 failed calls"],state:"proposed"}],experiments:[{id:"measured",title:"Validate earlier",hypothesis:"Catch issues sooner",recommendation_id:"tool-failures",baseline_session_id:"a",followup_session_id:"b",status:"measured",created_at:"2026-09-24T00:00:00Z",updated_at:"2026-09-25T00:00:00Z",baseline_title:"Alpha session",followup_title:"Beta session",measurement:{confidence:"observed",evidence:["Baseline session a","Follow-up session b"],deltas:[{metric:"Failed calls",baseline:3,followup:1,delta:-2,unit:"calls",interpretation:"Follow-up changed by -2.00 calls."}]}},{id:"running",title:"Use a checklist",baseline_session_id:"a",status:"running",created_at:"2026-09-25T00:00:00Z",updated_at:"2026-09-25T00:00:00Z"}],adoption:{active_days:2,optimize_views:3,experiments_created:2,recommendations_changed:1,reports_exported:0},privacy:["No conversation text is included."]};

describe("optimize",()=>{
  it("renders the complete loop and changes recommendation state",async()=>{
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL,init?:RequestInit)=>{
      if(String(input).endsWith("/adoption"))return json({recorded:true});
      if(String(input).endsWith("/recommendations"))return json({recommendation_id:"tool-failures",state:"accepted"});
      if(init?.method==="POST")return json({});
      return json(overview);
    }));
    render(<Optimize sessions={[first,second]}/>);
    expect(await screen.findByRole("heading",{name:"Turn patterns into a measurable change."})).toBeInTheDocument();
    expect(screen.getByText("Validate earlier")).toBeInTheDocument();
    expect(screen.getByText("-2 calls")).toBeInTheDocument();
    expect(screen.getByRole("link",{name:"Download Markdown"})).toHaveAttribute("href","/api/v1/optimization/report.md");
    fireEvent.change(screen.getByLabelText("State for Shorten failure loops"),{target:{value:"accepted"}});
    await waitFor(()=>expect(fetch).toHaveBeenCalledWith("/api/v1/optimization/recommendations",expect.objectContaining({method:"POST"})));
  });

  it("creates an experiment and attaches a follow-up",async()=>{
    const fetchMock=vi.fn((input:RequestInfo|URL,init?:RequestInit)=>{
      const path=String(input);
      if(path.endsWith("/adoption"))return json({recorded:true});
      if(path.endsWith("/experiments/update"))return json({...overview.experiments[1],followup_session_id:"b",status:"measured"});
      if(path.endsWith("/experiments")&&init?.method==="POST")return json({...overview.experiments[1],id:"new"},201);
      return json(overview);
    });
    vi.stubGlobal("fetch",fetchMock);
    render(<Optimize sessions={[first,second]}/>);
    await screen.findByText("Start an experiment");
    fireEvent.change(screen.getByLabelText("Experiment title"),{target:{value:"Prompt with acceptance criteria"}});
    fireEvent.change(screen.getByLabelText("Baseline session"),{target:{value:"a"}});
    fireEvent.click(screen.getByRole("button",{name:"Start experiment"}));
    await waitFor(()=>expect(fetchMock).toHaveBeenCalledWith("/api/v1/optimization/experiments",expect.objectContaining({method:"POST"})));
    fireEvent.change(screen.getByLabelText("Follow-up for Use a checklist"),{target:{value:"b"}});
    fireEvent.click(screen.getByRole("button",{name:"Measure Use a checklist"}));
    await waitFor(()=>expect(fetchMock).toHaveBeenCalledWith("/api/v1/optimization/experiments/update",expect.objectContaining({method:"POST"})));
  });

  it("explains empty state and reports API failures",async()=>{
    const empty={...overview,recommendations:[],experiments:[],summary:{...overview.summary,running_experiments:0,measured_experiments:0}};
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>String(input).endsWith("/adoption")?json({recorded:true}):json(empty)));
    const {unmount}=render(<Optimize sessions={[]}/>);
    await waitFor(()=>expect(screen.getByText("No experiments yet.")).toBeInTheDocument());
    unmount();
    vi.stubGlobal("fetch",vi.fn(()=>Promise.reject(new Error("offline"))));
    render(<Optimize sessions={[first]}/>);
    expect(await screen.findByRole("alert")).toHaveTextContent("offline");
  });
});
