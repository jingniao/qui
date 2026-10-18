CREATE TABLE node_ca_successor_retirement_assessments (
 plan_digest TEXT PRIMARY KEY REFERENCES node_ca_successor_rounds(plan_digest),
 readiness_json BLOB NOT NULL CHECK(length(readiness_json)>0 AND length(readiness_json)<=1048576),
 prepared_at TEXT NOT NULL
);