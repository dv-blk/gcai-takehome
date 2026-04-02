## How You Interpreted the Problem

My understanding of the issue is that they have:
- A SaaS company working for/signing with other businesses
- Various provided vendor contract files
- A desire to delegate their analysis for contract fairness
- A requirement of thorough and non-specific analysis

My goals for this problem:
- Provide a fluid, easy to understand and use interface
- Ensure common areas of unfair contract terms are clearly raised to the user
- Leverage a LLM to achieve this

Assumptions:
- The user understands and accepts the use of third party LLMs reviewing the contract and the implications thereof
- They want to analyze many contracts, and review previous analyses
- May need to consider token usage efficiencies in large contracts

---

## Key Product Decisions You Made and Why

**Decision 1**  
  - **What:** Use tool augmented LLM interface (Opencode/Claude code)  
  - **Why:** Any read efficiencies that can be achieved by the LLM using tool interfaces for large contracts may occur passively, session logic is built in  
  - **Trade-offs:** Added security implications, adding requirements to isolate the LLM agent from reading arbitrary files  

**Decision 2**  
  - **What:** Using Golang/HTMX frontend  
  - **Why:** Quick AI development loop (no browser required, simple syntax) very few dependencies  
  - **Trade-offs:** Added complexity to accommodate a javascript-only SDK, requiring an independent nodejs bridge process  

---

## What You'd Do Differently With More Time

**Improvement 1**  
  - **Change:** Add a general analysis category that would run last  
  - **Impact:** Search for any areas that may have been missed by all of our analysis categories  

**Improvement 2**  
  - **Change:** There is likely a better tool augmented solution than opencode for reviewing legal text (likely conflicting system level prompts geared toward software creation; requiring plain text on disk; custom read tools oriented toward legal contracts). Recording actions from the LLM and finding areas to optimize or improve the stack (prompt/pre parsing/tagging/regression testing) would be fundamental continuing forward.  
  - **Impact:** Clear real world efficiency gains in token usage  

**Improvement 3**  
  - **Change:** Basic security considerations - CORS, CSRF, JWT, XSS protection, plain text storage, backend input validation, plain database ids in URL query string, etc.  
  - **Impact:** Protecting user information from basic vulnerabilities  

**Improvement 4**  
  - **Change:** Better UX handling contracts that are fair (outlining the qualities of its fairness)  
  - **Impact:** More feedback to the user to assure them when there is a lack of signal from the aggressive clause analysis  

**Improvement 5**  
  - **Change:** UI refinements, show clause paragraph number for results (pages as well for pdf); better pdf rendering for contract text in the main view; UI re-renders close the contract content while streaming (bug); submission progress update when not in the current view; add delete; bulk contract upload(?); etc.  
  - **Impact:** Progressive polish of a product that is user facing in a service domain where appearances will influence perception of prospective clients (excuse my alliteration!).

---

## What You're Most / Least Confident About

### Most Confident

**Area 1**  
  - **What:** General Implementation, Proof-of-Concept  
  - **Why:** The app fulfills its purpose of identifying aggressive terms and displaying them as such, in a UI that enables user to reference previous contracts.  

**Area 2**  
  - **What:** Contract Analysis  
  - **Why:** Results were consistent across several runs, and provides pertinent findings across many common legal term categories

**Area 3**  
  - **What:** DX, Reproducibility 
  - **Why:** This project was tested on multiple computers, has minimal dependencies, setup only requires Docker and a GitHub copilot key.  

### Least Confident

**Area 1**  
  - **What:** Token usage at scale  
  - **Why:** This remains my concern from before I started and after completing the project. Adding tool augments and sessions were my attempts at ameliorating the issue, but more testing remains before finalizing an LLM integration layer.  

**Area 2**  
  - **What:** Adoption of a Golang/Htmx solution  
  - **Why:** A caveat: the majority of my career has centered around Javascript and Typescript, and am quite comfortable creating and debugging the entire stack. While Golang and HTMX are simple solutions, I use them in this project for expedience and prototyping reasons. Unless there is a precedent of using these, I am likely going against the grain, eg. full stack typescript, for client facing apps.  