/** @module ChangelogTest @description Anchors semantic release rendering and server-backed pagination controls. */
import "@testing-library/jest-dom/vitest";
import React from "react";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,expect,it,vi} from "vitest";
import {Changelog} from "./Changelog";
import {json} from "./testFixtures";

afterEach(()=>{cleanup();vi.unstubAllGlobals()});

it("renders the current semantic version and pages through release history",async()=>{
  vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>String(input).includes("page=2")
    ?json({current_version:"2.0.0",page:2,page_size:1,total:10,total_pages:10,has_previous:true,has_next:true,releases:[{version:"1.8.0",date:"2026-09-25",title:"Personal optimization and session provenance",changes:[{category:"Added",items:["Local optimization loop"]}]}]})
    :json({current_version:"2.0.0",page:1,page_size:1,total:10,total_pages:10,has_previous:false,has_next:true,releases:[{version:"2.0.0",date:"2026-10-05",title:"Stable Docker service and visible release history",changes:[{category:"Added",items:["Paginated changelog"]}]}]})));
  render(<Changelog/>);
  expect(await screen.findByRole("heading",{name:"Stable Docker service and visible release history"})).toBeInTheDocument();
  expect(screen.getByText("Current version 2.0.0")).toBeInTheDocument();
  expect(screen.getByText("Page 1 of 10")).toBeInTheDocument();
  expect(screen.getByRole("button",{name:"Previous releases"})).toBeDisabled();
  fireEvent.click(screen.getByRole("button",{name:"Next releases"}));
  expect(await screen.findByRole("heading",{name:"Personal optimization and session provenance"})).toBeInTheDocument();
  await waitFor(()=>expect(screen.getByText("Page 2 of 10")).toBeInTheDocument());
  expect(screen.getByRole("button",{name:"Previous releases"})).toBeEnabled();
});

it("reports changelog loading failures",async()=>{
  vi.stubGlobal("fetch",vi.fn(()=>Promise.reject(new Error("offline"))));
  render(<Changelog/>);
  expect(await screen.findByRole("alert")).toHaveTextContent("offline");
});
