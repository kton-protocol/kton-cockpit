package ktonmcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A guided tour through kton-web's pmx corpus (ten NONMEM model developments, six analysts, their
// reviews, QC and reproductions) as an MCP prompt: the person starts it in their assistant, and the
// assistant leads stop by stop, asking kton (kton_sparql, kton_ask) from an explore repository that
// reads pmx and showing what it finds in kton-web (kton_open_graph, kton_select). The queries were
// tried on that corpus with cockpit v0.4.0-rc2; the overview (stop 1) is a result, since computing
// it takes over a minute.

func addTour(s *mcp.Server) {
	s.AddPrompt(&mcp.Prompt{
		Name:        "pmx_tour",
		Title:       "A guided tour through the pmx corpus",
		Description: "Leads you through kton-web's pmx corpus (ten model developments by six analysts, with reviews, QC and reproductions): what is there, how one analysis reached its final model, the decisions and their reasons, the checks, and where your own work sits. Shows each answer in kton-web.",
		Arguments:   []*mcp.PromptArgument{{Name: "explore", Description: "the explore repository that reads pmx (default ~/kton-programme/explore)"}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		explore := req.Params.Arguments["explore"]
		if explore == "" {
			home, _ := os.UserHomeDir()
			explore = filepath.Join(home, "kton-programme", "explore")
		}
		return &mcp.GetPromptResult{
			Description: "pmx tour",
			Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: strings.ReplaceAll(tourText, "EXPLORE", explore)}}},
		}, nil
	})
}

const tourPrefixes = `PREFIX prov: <http://www.w3.org/ns/prov#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX dct: <http://purl.org/dc/terms/>
PREFIX nk: <https://kton.dev/v/>
PREFIX lab: <https://kton.dev/v/lab/>
PREFIX pmx: <https://kton.dev/v/pmx/>`

var tourText = `Take me on a short guided tour through the pmx corpus in kton. Lead it one stop at a time: say what the stop is about in one or two sentences, do it (ask, then show), tell me what to look at in the graph, and wait for me before the next stop. Keep the answers short; I can ask for more. Ask everything with project = "EXPLORE" (a repository that reads and trusts pmx and my programme). Every answer here is for exploring: nothing signed rests on it.

What pmx is: kton-web's example corpus of population PK model development: ten analyses PMX-01 … PMX-10 on the same data (data/adpc.csv, data/adsl.csv), NONMEM runs (nmfe) developed from one another (parent-step), scripts adapted from a house skeleton (based-on), base and final models and key steps marked, reviews (initialised, comment, response, approve/reject, closed), QC passes, independent reproductions, and papers that rest on models.

Signers (keyid → who): a8815475584391c7 anna · b9e63840e03a3760 bruno · c95445a11cf7e4ea clara · b17fee204254a8a7 dmitri · 750e6d3d7623beb1 elif · 16cc498c693b66b0 farid · 741fd7e2b461aa90 lead · d9486c988ab259af qc-anna · 28306fb563f6b653 qc-bruno · 1342865d81bb41f3 qc-clara · 7eb6be3ea46027d2 qc-dmitri · d0527c56246df9c4 qc-elif · a6d502827f85c383 qc-farid. Keys not in this list belong to my own programme (my analysis, Ben, Carla, Dmitri of THP/CX).

Use these prefixes in every query:
` + tourPrefixes + `

Stop 1 — What is here. Do not query (it takes over a minute); this is the overview, from a survey of the corpus:
PMX-01 172 steps (clara, farid, elif) · PMX-02 97 (elif, dmitri, farid) · PMX-03 132 (elif, anna, dmitri) · PMX-04 369 (bruno, farid, elif) · PMX-05 210 (anna, bruno, dmitri) · PMX-06 170 (clara, elif, anna) · PMX-07 360 (dmitri, elif, farid) · PMX-08 294 (farid, anna, elif) · PMX-09 294 (farid, elif, anna) · PMX-10 246 (farid, elif, clara). About 1600 steps, 2900 claims.
Then ask the final model of each analysis and show it as a table:
SELECT DISTINCT ?analysis ?model ?file (STRAFTER(STR(?f), "/o/") AS ?hash) WHERE { ?c nk:predicate pmx:final-model ; nk:about ?step ; lab:value ?model ; nk:current true . ?f prov:wasGeneratedBy ?step ; rdfs:label ?file FILTER(STRSTARTS(?file, "models/PMX-") && STRENDS(?file, ".fit") && !CONTAINS(?file, "parent")) BIND(SUBSTR(?file, 8, 6) AS ?analysis) } ORDER BY ?analysis
Keep the hashes: they are where the next stops start.

Stop 2 — One development. Open PMX-02's final model in the graph: kton_open_graph with repo = "EXPLORE", hash = sha256:<the hash of models/PMX-02/run067-cov-bwd-SEX-on-V.fit from stop 1>, title "PMX-02: final model". Tell me: the graph is laid out top-down along "developed from" (parent-step), so the earliest runs are at the top and the final model at the bottom; KEY, BASE and FINAL are marks on the steps; the dashed "adapted from" edges go to the house skeleton. Let me look and click around.

Stop 3 — The decisions. Which steps of PMX-02 were marked key, base and final:
SELECT DISTINCT ?kind ?value ?why ?file (STRAFTER(STR(?step), "/o/") AS ?stepId) WHERE { VALUES (?pred ?kind) { (pmx:key-step "key") (pmx:base-model "base") (pmx:final-model "final") } ?c nk:predicate ?pred ; nk:about ?step ; lab:value ?value ; nk:current true . OPTIONAL { ?c lab:why ?why } ?f prov:wasGeneratedBy ?step ; rdfs:label ?file FILTER(STRSTARTS(?file, "models/PMX-02/") && STRENDS(?file, ".fit") && !CONTAINS(?file, "parent")) } ORDER BY ?kind
Summarise the path (base model → key steps such as random effects → final covariate model), with the reasons the analyst gave (why), and select those steps in the graph: kton_select with project = "EXPLORE" and the step ids as sha256:<stepId>.

Stop 4 — The review. Was a final model reviewed, and how did it go:
SELECT ?kind ?value ?why ?who ?when (STRAFTER(STR(?s), "/o/") AS ?subject) WHERE { VALUES ?p { nk:review-initialised nk:review-comment nk:review-response nk:reviewed nk:closed } ?c nk:predicate ?p ; nk:about ?s ; lab:value ?value ; dct:created ?when ; nk:signer ?a . OPTIONAL { ?c lab:why ?why } OPTIONAL { ?a rdfs:label ?who } BIND(STRAFTER(STR(?p), "https://kton.dev/v/") AS ?kind) } ORDER BY ?when LIMIT 40
Tell the story of one review in the reviewers' and the analyst's own words (why): opened → comments → responses → approve or reject → closed, and of one that was rejected.

Stop 5 — Independent checks. Who checked whose work:
SELECT DISTINCT ?check ?value ?file ?by WHERE { VALUES (?pred ?check) { (nk:reproduces "reproduced") (pmx:qc-passed "qc passed") } ?c nk:predicate ?pred ; nk:about ?step ; nk:signer ?a . OPTIONAL { ?c lab:value ?value } OPTIONAL { ?a rdfs:label ?by } OPTIONAL { ?f prov:wasGeneratedBy ?step ; rdfs:label ?file } } ORDER BY ?check
?by is the signer's name (qc-* keys check others' analyses). In the graph a QC pass is a ✓ on the step. For one reproduced file, kton_ask reproductions on its hash shows how many independent signers produced it.

Stop 6 — What rests on a model. Papers and reports and the runs they draw on:
SELECT DISTINCT ?report ?restsOn WHERE { ?c nk:predicate pmx:rests-on ; nk:about ?step ; lab:rests-on ?x . ?x lab:hash ?t . ?rf prov:wasGeneratedBy ?step ; rdfs:label ?report OPTIONAL { ?mf prov:wasGeneratedBy ?t ; rdfs:label ?restsOn FILTER(STRENDS(?restsOn, ".fit") && !CONTAINS(?restsOn, "parent")) } } ORDER BY ?report LIMIT 40
Point out that a paper's claim reaches back through the model to the data (kton_ask lineage on the paper's hash if I want the whole chain).

Stop 7 — Across the analyses. Compare the final models of stop 1: which covariate on which parameter (AGE on CL, SEX on V, WT on CL, CRCL on CL …), how many analyses chose each. Ask me what I make of it: the same data, different final models.

Stop 8 — Where I am. My own programme is in the same store: my THP-101 analysis and Ben's share pk.csv, Carla adapted my prepare script, Dmitri reproduced Carla. Offer to open my programme's graph (kton_open_graph with my project, or repo = "EXPLORE" and a hash of mine), and say that kton_copy can take a script from pmx or from Ben into my project, asking me first.

End by asking which stop I want to go deeper into. If a query is refused (a cockpit before v0.4.0-rc5 has no claims as nodes), say so briefly and fall back to kton_ask.`
