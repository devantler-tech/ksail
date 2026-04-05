window.BENCHMARK_DATA = {
  "lastUpdate": 1775429035531,
  "repoUrl": "https://github.com/devantler-tech/ksail",
  "entries": {
    "Benchmark": [
      {
        "commit": {
          "author": {
            "email": "ned@devantler.tech",
            "name": "Nikolai Emil Damm",
            "username": "devantler"
          },
          "committer": {
            "email": "noreply@github.com",
            "name": "GitHub",
            "username": "web-flow"
          },
          "distinct": true,
          "id": "67642894424922343596cf1d286244fd6b44fb3a",
          "message": "refactor: use table format for cluster list output (#3720)\n\n* refactor: use table format for cluster list output\n\nReplace annotation-style output with aligned table columns\n(PROVIDER, DISTRIBUTION, CLUSTER, TTL) for cleaner, scannable\noutput without name duplication.\n\n- Refactor displayListResults to compute dynamic column widths\n- Remove formatAnnotationLabel and ttlIndent (no longer needed)\n- TTL column only shown when at least one cluster has a TTL set\n- Update CI system test parsing to extract from table columns\n- Update test snapshots and remove obsolete tests\n\nFixes #3704\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>\n\n* chore: sync modules and update generated files\n\n* fix: address review feedback for table format\n\n- Update displayListResults comment to describe table format\n- Add TTL display tests (with TTL, expired TTL, no TTL column)\n- Add ExportDisplayListResults and ExportNewListResult test seams\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>\n\n* fix: use regexp assertion for TTL test to avoid minute-boundary flakiness\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>\n\n* fix: avoid trailing whitespace on rows without TTL\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>\n\n* refactor: extract table helpers to reduce cyclomatic complexity\n\nSplit displayListResults into buildTableRows, formatTTLValue,\nprintTable, and printTableRow. Rename short variable 'r' to 'result'.\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>\n\n* fix: consistent column alignment and robust TTL test buffer\n\n- Pass hasTTLColumn to printTableRow so rows without TTL still\n  pad the CLUSTER column consistently when the TTL header is shown\n- Increase TTL test buffer to 5h and use loose regex to eliminate\n  any minute-boundary flakiness\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>\n\n---------\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>\nCo-authored-by: devantler <26203420+devantler@users.noreply.github.com>",
          "timestamp": "2026-04-06T00:33:31+02:00",
          "tree_id": "71b4bbcd2c19e25d794a296e308b03def45e7a56",
          "url": "https://github.com/devantler-tech/ksail/commit/67642894424922343596cf1d286244fd6b44fb3a"
        },
        "date": 1775429034557,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkCluster_MarshalYAML/Minimal",
            "value": 73135,
            "unit": "ns/op\t    8321 B/op\t     212 allocs/op",
            "extra": "17151 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/Minimal - ns/op",
            "value": 73135,
            "unit": "ns/op",
            "extra": "17151 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/Minimal - B/op",
            "value": 8321,
            "unit": "B/op",
            "extra": "17151 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/Minimal - allocs/op",
            "value": 212,
            "unit": "allocs/op",
            "extra": "17151 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithBasicConfig",
            "value": 79642,
            "unit": "ns/op\t    8320 B/op\t     212 allocs/op",
            "extra": "15948 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithBasicConfig - ns/op",
            "value": 79642,
            "unit": "ns/op",
            "extra": "15948 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithBasicConfig - B/op",
            "value": 8320,
            "unit": "B/op",
            "extra": "15948 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithBasicConfig - allocs/op",
            "value": 212,
            "unit": "allocs/op",
            "extra": "15948 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithCNI",
            "value": 85326,
            "unit": "ns/op\t    8912 B/op\t     215 allocs/op",
            "extra": "14306 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithCNI - ns/op",
            "value": 85326,
            "unit": "ns/op",
            "extra": "14306 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithCNI - B/op",
            "value": 8912,
            "unit": "B/op",
            "extra": "14306 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithCNI - allocs/op",
            "value": 215,
            "unit": "allocs/op",
            "extra": "14306 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithGitOps",
            "value": 89848,
            "unit": "ns/op\t    9232 B/op\t     218 allocs/op",
            "extra": "12948 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithGitOps - ns/op",
            "value": 89848,
            "unit": "ns/op",
            "extra": "12948 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithGitOps - B/op",
            "value": 9232,
            "unit": "B/op",
            "extra": "12948 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/WithGitOps - allocs/op",
            "value": 218,
            "unit": "allocs/op",
            "extra": "12948 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/FullProductionCluster",
            "value": 97981,
            "unit": "ns/op\t   11256 B/op\t     242 allocs/op",
            "extra": "12367 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/FullProductionCluster - ns/op",
            "value": 97981,
            "unit": "ns/op",
            "extra": "12367 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/FullProductionCluster - B/op",
            "value": 11256,
            "unit": "B/op",
            "extra": "12367 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalYAML/FullProductionCluster - allocs/op",
            "value": 242,
            "unit": "allocs/op",
            "extra": "12367 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/Minimal",
            "value": 85372,
            "unit": "ns/op\t    8530 B/op\t     218 allocs/op",
            "extra": "14358 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/Minimal - ns/op",
            "value": 85372,
            "unit": "ns/op",
            "extra": "14358 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/Minimal - B/op",
            "value": 8530,
            "unit": "B/op",
            "extra": "14358 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/Minimal - allocs/op",
            "value": 218,
            "unit": "allocs/op",
            "extra": "14358 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/WithBasicConfig",
            "value": 88816,
            "unit": "ns/op\t    8530 B/op\t     218 allocs/op",
            "extra": "13676 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/WithBasicConfig - ns/op",
            "value": 88816,
            "unit": "ns/op",
            "extra": "13676 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/WithBasicConfig - B/op",
            "value": 8530,
            "unit": "B/op",
            "extra": "13676 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/WithBasicConfig - allocs/op",
            "value": 218,
            "unit": "allocs/op",
            "extra": "13676 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/FullProductionCluster",
            "value": 126631,
            "unit": "ns/op\t   14214 B/op\t     313 allocs/op",
            "extra": "10000 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/FullProductionCluster - ns/op",
            "value": 126631,
            "unit": "ns/op",
            "extra": "10000 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/FullProductionCluster - B/op",
            "value": 14214,
            "unit": "B/op",
            "extra": "10000 times\n4 procs"
          },
          {
            "name": "BenchmarkCluster_MarshalJSON/FullProductionCluster - allocs/op",
            "value": 313,
            "unit": "allocs/op",
            "extra": "10000 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLEncode/Minimal",
            "value": 126962,
            "unit": "ns/op\t   15256 B/op\t     240 allocs/op",
            "extra": "9871 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLEncode/Minimal - ns/op",
            "value": 126962,
            "unit": "ns/op",
            "extra": "9871 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLEncode/Minimal - B/op",
            "value": 15256,
            "unit": "B/op",
            "extra": "9871 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLEncode/Minimal - allocs/op",
            "value": 240,
            "unit": "allocs/op",
            "extra": "9871 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLEncode/FullProductionCluster",
            "value": 136285,
            "unit": "ns/op\t   26384 B/op\t     285 allocs/op",
            "extra": "7974 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLEncode/FullProductionCluster - ns/op",
            "value": 136285,
            "unit": "ns/op",
            "extra": "7974 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLEncode/FullProductionCluster - B/op",
            "value": 26384,
            "unit": "B/op",
            "extra": "7974 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLEncode/FullProductionCluster - allocs/op",
            "value": 285,
            "unit": "allocs/op",
            "extra": "7974 times\n4 procs"
          },
          {
            "name": "BenchmarkJSONEncode",
            "value": 95077,
            "unit": "ns/op\t    9766 B/op\t     236 allocs/op",
            "extra": "12358 times\n4 procs"
          },
          {
            "name": "BenchmarkJSONEncode - ns/op",
            "value": 95077,
            "unit": "ns/op",
            "extra": "12358 times\n4 procs"
          },
          {
            "name": "BenchmarkJSONEncode - B/op",
            "value": 9766,
            "unit": "B/op",
            "extra": "12358 times\n4 procs"
          },
          {
            "name": "BenchmarkJSONEncode - allocs/op",
            "value": 236,
            "unit": "allocs/op",
            "extra": "12358 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/MostlyDefaults",
            "value": 49536,
            "unit": "ns/op\t    4464 B/op\t     136 allocs/op",
            "extra": "24328 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/MostlyDefaults - ns/op",
            "value": 49536,
            "unit": "ns/op",
            "extra": "24328 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/MostlyDefaults - B/op",
            "value": 4464,
            "unit": "B/op",
            "extra": "24328 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/MostlyDefaults - allocs/op",
            "value": 136,
            "unit": "allocs/op",
            "extra": "24328 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/MixedDefaultsAndCustom",
            "value": 46424,
            "unit": "ns/op\t    4464 B/op\t     136 allocs/op",
            "extra": "26506 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/MixedDefaultsAndCustom - ns/op",
            "value": 46424,
            "unit": "ns/op",
            "extra": "26506 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/MixedDefaultsAndCustom - B/op",
            "value": 4464,
            "unit": "B/op",
            "extra": "26506 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/MixedDefaultsAndCustom - allocs/op",
            "value": 136,
            "unit": "allocs/op",
            "extra": "26506 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/AllCustomValues",
            "value": 42094,
            "unit": "ns/op\t    4464 B/op\t     136 allocs/op",
            "extra": "28228 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/AllCustomValues - ns/op",
            "value": 42094,
            "unit": "ns/op",
            "extra": "28228 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/AllCustomValues - B/op",
            "value": 4464,
            "unit": "B/op",
            "extra": "28228 times\n4 procs"
          },
          {
            "name": "BenchmarkPruneClusterDefaults/AllCustomValues - allocs/op",
            "value": 136,
            "unit": "allocs/op",
            "extra": "28228 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Minimal",
            "value": 755604,
            "unit": "ns/op\t  126202 B/op\t     636 allocs/op",
            "extra": "1543 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Minimal - ns/op",
            "value": 755604,
            "unit": "ns/op",
            "extra": "1543 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Minimal - B/op",
            "value": 126202,
            "unit": "B/op",
            "extra": "1543 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Minimal - allocs/op",
            "value": 636,
            "unit": "allocs/op",
            "extra": "1543 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Small",
            "value": 1264183,
            "unit": "ns/op\t  399038 B/op\t    1889 allocs/op",
            "extra": "873 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Small - ns/op",
            "value": 1264183,
            "unit": "ns/op",
            "extra": "873 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Small - B/op",
            "value": 399038,
            "unit": "B/op",
            "extra": "873 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Small - allocs/op",
            "value": 1889,
            "unit": "allocs/op",
            "extra": "873 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Medium",
            "value": 2599310,
            "unit": "ns/op\t  902607 B/op\t    4068 allocs/op",
            "extra": "564 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Medium - ns/op",
            "value": 2599310,
            "unit": "ns/op",
            "extra": "564 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Medium - B/op",
            "value": 902607,
            "unit": "B/op",
            "extra": "564 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Medium - allocs/op",
            "value": 4068,
            "unit": "allocs/op",
            "extra": "564 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Large",
            "value": 9732420,
            "unit": "ns/op\t 3303139 B/op\t   14856 allocs/op",
            "extra": "124 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Large - ns/op",
            "value": 9732420,
            "unit": "ns/op",
            "extra": "124 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Large - B/op",
            "value": 3303139,
            "unit": "B/op",
            "extra": "124 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Large - allocs/op",
            "value": 14856,
            "unit": "allocs/op",
            "extra": "124 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Nested",
            "value": 2496805,
            "unit": "ns/op\t  801732 B/op\t    3722 allocs/op",
            "extra": "513 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Nested - ns/op",
            "value": 2496805,
            "unit": "ns/op",
            "extra": "513 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Nested - B/op",
            "value": 801732,
            "unit": "B/op",
            "extra": "513 times\n4 procs"
          },
          {
            "name": "BenchmarkEncrypt/Nested - allocs/op",
            "value": 3722,
            "unit": "allocs/op",
            "extra": "513 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Minimal",
            "value": 1005930,
            "unit": "ns/op\t  240378 B/op\t     670 allocs/op",
            "extra": "1351 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Minimal - ns/op",
            "value": 1005930,
            "unit": "ns/op",
            "extra": "1351 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Minimal - B/op",
            "value": 240378,
            "unit": "B/op",
            "extra": "1351 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Minimal - allocs/op",
            "value": 670,
            "unit": "allocs/op",
            "extra": "1351 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Small",
            "value": 1748679,
            "unit": "ns/op\t  500209 B/op\t    1886 allocs/op",
            "extra": "730 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Small - ns/op",
            "value": 1748679,
            "unit": "ns/op",
            "extra": "730 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Small - B/op",
            "value": 500209,
            "unit": "B/op",
            "extra": "730 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Small - allocs/op",
            "value": 1886,
            "unit": "allocs/op",
            "extra": "730 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Medium",
            "value": 3163338,
            "unit": "ns/op\t  975754 B/op\t    4026 allocs/op",
            "extra": "394 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Medium - ns/op",
            "value": 3163338,
            "unit": "ns/op",
            "extra": "394 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Medium - B/op",
            "value": 975754,
            "unit": "B/op",
            "extra": "394 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Medium - allocs/op",
            "value": 4026,
            "unit": "allocs/op",
            "extra": "394 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Large",
            "value": 11137137,
            "unit": "ns/op\t 3384710 B/op\t   14653 allocs/op",
            "extra": "100 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Large - ns/op",
            "value": 11137137,
            "unit": "ns/op",
            "extra": "100 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Large - B/op",
            "value": 3384710,
            "unit": "B/op",
            "extra": "100 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Large - allocs/op",
            "value": 14653,
            "unit": "allocs/op",
            "extra": "100 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Nested",
            "value": 2919743,
            "unit": "ns/op\t  921691 B/op\t    3660 allocs/op",
            "extra": "439 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Nested - ns/op",
            "value": 2919743,
            "unit": "ns/op",
            "extra": "439 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Nested - B/op",
            "value": 921691,
            "unit": "B/op",
            "extra": "439 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/Nested - allocs/op",
            "value": 3660,
            "unit": "allocs/op",
            "extra": "439 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/WithExtract",
            "value": 1913775,
            "unit": "ns/op\t  318279 B/op\t    1822 allocs/op",
            "extra": "646 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/WithExtract - ns/op",
            "value": 1913775,
            "unit": "ns/op",
            "extra": "646 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/WithExtract - B/op",
            "value": 318279,
            "unit": "B/op",
            "extra": "646 times\n4 procs"
          },
          {
            "name": "BenchmarkDecrypt/WithExtract - allocs/op",
            "value": 1822,
            "unit": "allocs/op",
            "extra": "646 times\n4 procs"
          },
          {
            "name": "BenchmarkRoundtrip_Minimal",
            "value": 1343225,
            "unit": "ns/op\t  367720 B/op\t    1310 allocs/op",
            "extra": "745 times\n4 procs"
          },
          {
            "name": "BenchmarkRoundtrip_Minimal - ns/op",
            "value": 1343225,
            "unit": "ns/op",
            "extra": "745 times\n4 procs"
          },
          {
            "name": "BenchmarkRoundtrip_Minimal - B/op",
            "value": 367720,
            "unit": "B/op",
            "extra": "745 times\n4 procs"
          },
          {
            "name": "BenchmarkRoundtrip_Minimal - allocs/op",
            "value": 1310,
            "unit": "allocs/op",
            "extra": "745 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_SinglePod",
            "value": 137771,
            "unit": "ns/op\t  117531 B/op\t     939 allocs/op",
            "extra": "8275 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_SinglePod - ns/op",
            "value": 137771,
            "unit": "ns/op",
            "extra": "8275 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_SinglePod - B/op",
            "value": 117531,
            "unit": "B/op",
            "extra": "8275 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_SinglePod - allocs/op",
            "value": 939,
            "unit": "allocs/op",
            "extra": "8275 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_PodList",
            "value": 224833,
            "unit": "ns/op\t  186736 B/op\t    1637 allocs/op",
            "extra": "5479 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_PodList - ns/op",
            "value": 224833,
            "unit": "ns/op",
            "extra": "5479 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_PodList - B/op",
            "value": 186736,
            "unit": "B/op",
            "extra": "5479 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_PodList - allocs/op",
            "value": 1637,
            "unit": "allocs/op",
            "extra": "5479 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_NonYAML",
            "value": 5044,
            "unit": "ns/op\t    5568 B/op\t      45 allocs/op",
            "extra": "234402 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_NonYAML - ns/op",
            "value": 5044,
            "unit": "ns/op",
            "extra": "234402 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_NonYAML - B/op",
            "value": 5568,
            "unit": "B/op",
            "extra": "234402 times\n4 procs"
          },
          {
            "name": "BenchmarkSanitizeYAMLOutput_NonYAML - allocs/op",
            "value": 45,
            "unit": "allocs/op",
            "extra": "234402 times\n4 procs"
          },
          {
            "name": "BenchmarkCountYAMLDocuments_Single",
            "value": 49.5,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "24008674 times\n4 procs"
          },
          {
            "name": "BenchmarkCountYAMLDocuments_Single - ns/op",
            "value": 49.5,
            "unit": "ns/op",
            "extra": "24008674 times\n4 procs"
          },
          {
            "name": "BenchmarkCountYAMLDocuments_Single - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "24008674 times\n4 procs"
          },
          {
            "name": "BenchmarkCountYAMLDocuments_Single - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "24008674 times\n4 procs"
          },
          {
            "name": "BenchmarkCountYAMLDocuments_List",
            "value": 613.5,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "1954036 times\n4 procs"
          },
          {
            "name": "BenchmarkCountYAMLDocuments_List - ns/op",
            "value": 613.5,
            "unit": "ns/op",
            "extra": "1954036 times\n4 procs"
          },
          {
            "name": "BenchmarkCountYAMLDocuments_List - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "1954036 times\n4 procs"
          },
          {
            "name": "BenchmarkCountYAMLDocuments_List - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "1954036 times\n4 procs"
          },
          {
            "name": "BenchmarkFilterExcludedTypes_NoExclusions",
            "value": 636.3,
            "unit": "ns/op\t    1008 B/op\t       6 allocs/op",
            "extra": "1886409 times\n4 procs"
          },
          {
            "name": "BenchmarkFilterExcludedTypes_NoExclusions - ns/op",
            "value": 636.3,
            "unit": "ns/op",
            "extra": "1886409 times\n4 procs"
          },
          {
            "name": "BenchmarkFilterExcludedTypes_NoExclusions - B/op",
            "value": 1008,
            "unit": "B/op",
            "extra": "1886409 times\n4 procs"
          },
          {
            "name": "BenchmarkFilterExcludedTypes_NoExclusions - allocs/op",
            "value": 6,
            "unit": "allocs/op",
            "extra": "1886409 times\n4 procs"
          },
          {
            "name": "BenchmarkFilterExcludedTypes_DefaultExclusions",
            "value": 832.4,
            "unit": "ns/op\t    1007 B/op\t       5 allocs/op",
            "extra": "1442997 times\n4 procs"
          },
          {
            "name": "BenchmarkFilterExcludedTypes_DefaultExclusions - ns/op",
            "value": 832.4,
            "unit": "ns/op",
            "extra": "1442997 times\n4 procs"
          },
          {
            "name": "BenchmarkFilterExcludedTypes_DefaultExclusions - B/op",
            "value": 1007,
            "unit": "B/op",
            "extra": "1442997 times\n4 procs"
          },
          {
            "name": "BenchmarkFilterExcludedTypes_DefaultExclusions - allocs/op",
            "value": 5,
            "unit": "allocs/op",
            "extra": "1442997 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateTarball_Small",
            "value": 431209,
            "unit": "ns/op\t  921433 B/op\t     172 allocs/op",
            "extra": "2793 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateTarball_Small - ns/op",
            "value": 431209,
            "unit": "ns/op",
            "extra": "2793 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateTarball_Small - B/op",
            "value": 921433,
            "unit": "B/op",
            "extra": "2793 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateTarball_Small - allocs/op",
            "value": 172,
            "unit": "allocs/op",
            "extra": "2793 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateTarball_Medium",
            "value": 1370873,
            "unit": "ns/op\t 1510805 B/op\t     770 allocs/op",
            "extra": "906 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateTarball_Medium - ns/op",
            "value": 1370873,
            "unit": "ns/op",
            "extra": "906 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateTarball_Medium - B/op",
            "value": 1510805,
            "unit": "B/op",
            "extra": "906 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateTarball_Medium - allocs/op",
            "value": 770,
            "unit": "allocs/op",
            "extra": "906 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_SingleChange",
            "value": 1549,
            "unit": "ns/op\t     616 B/op\t      16 allocs/op",
            "extra": "933538 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_SingleChange - ns/op",
            "value": 1549,
            "unit": "ns/op",
            "extra": "933538 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_SingleChange - B/op",
            "value": 616,
            "unit": "B/op",
            "extra": "933538 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_SingleChange - allocs/op",
            "value": 16,
            "unit": "allocs/op",
            "extra": "933538 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_SmallDiff",
            "value": 2579,
            "unit": "ns/op\t    1128 B/op\t      26 allocs/op",
            "extra": "584619 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_SmallDiff - ns/op",
            "value": 2579,
            "unit": "ns/op",
            "extra": "584619 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_SmallDiff - B/op",
            "value": 1128,
            "unit": "B/op",
            "extra": "584619 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_SmallDiff - allocs/op",
            "value": 26,
            "unit": "allocs/op",
            "extra": "584619 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_MixedCategories",
            "value": 3892,
            "unit": "ns/op\t    2064 B/op\t      36 allocs/op",
            "extra": "340796 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_MixedCategories - ns/op",
            "value": 3892,
            "unit": "ns/op",
            "extra": "340796 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_MixedCategories - B/op",
            "value": 2064,
            "unit": "B/op",
            "extra": "340796 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_MixedCategories - allocs/op",
            "value": 36,
            "unit": "allocs/op",
            "extra": "340796 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_LargeDiff",
            "value": 6220,
            "unit": "ns/op\t    3120 B/op\t      60 allocs/op",
            "extra": "184040 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_LargeDiff - ns/op",
            "value": 6220,
            "unit": "ns/op",
            "extra": "184040 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_LargeDiff - B/op",
            "value": 3120,
            "unit": "B/op",
            "extra": "184040 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_LargeDiff - allocs/op",
            "value": 60,
            "unit": "allocs/op",
            "extra": "184040 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_WideValues",
            "value": 2214,
            "unit": "ns/op\t    1352 B/op\t      21 allocs/op",
            "extra": "470560 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_WideValues - ns/op",
            "value": 2214,
            "unit": "ns/op",
            "extra": "470560 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_WideValues - B/op",
            "value": 1352,
            "unit": "B/op",
            "extra": "470560 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatDiffTable_WideValues - allocs/op",
            "value": 21,
            "unit": "allocs/op",
            "extra": "470560 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/Minimal",
            "value": 3.461,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "346541113 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/Minimal - ns/op",
            "value": 3.461,
            "unit": "ns/op",
            "extra": "346541113 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/Minimal - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "346541113 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/Minimal - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "346541113 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/WithApplicationName",
            "value": 1.847,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "649956884 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/WithApplicationName - ns/op",
            "value": 1.847,
            "unit": "ns/op",
            "extra": "649956884 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/WithApplicationName - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "649956884 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/WithApplicationName - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "649956884 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/WithAuth",
            "value": 2.1,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "571616302 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/WithAuth - ns/op",
            "value": 2.1,
            "unit": "ns/op",
            "extra": "571616302 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/WithAuth - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "571616302 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/WithAuth - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "571616302 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/Production",
            "value": 1.846,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "650531877 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/Production - ns/op",
            "value": 1.846,
            "unit": "ns/op",
            "extra": "650531877 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/Production - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "650531877 times\n4 procs"
          },
          {
            "name": "BenchmarkEnsureOptions/Production - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "650531877 times\n4 procs"
          },
          {
            "name": "BenchmarkUpdateTargetRevisionOptions/MinimalUpdate",
            "value": 1.748,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "686453128 times\n4 procs"
          },
          {
            "name": "BenchmarkUpdateTargetRevisionOptions/MinimalUpdate - ns/op",
            "value": 1.748,
            "unit": "ns/op",
            "extra": "686453128 times\n4 procs"
          },
          {
            "name": "BenchmarkUpdateTargetRevisionOptions/MinimalUpdate - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "686453128 times\n4 procs"
          },
          {
            "name": "BenchmarkUpdateTargetRevisionOptions/MinimalUpdate - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "686453128 times\n4 procs"
          },
          {
            "name": "BenchmarkUpdateTargetRevisionOptions/WithHardRefresh",
            "value": 1.986,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "606758926 times\n4 procs"
          },
          {
            "name": "BenchmarkUpdateTargetRevisionOptions/WithHardRefresh - ns/op",
            "value": 1.986,
            "unit": "ns/op",
            "extra": "606758926 times\n4 procs"
          },
          {
            "name": "BenchmarkUpdateTargetRevisionOptions/WithHardRefresh - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "606758926 times\n4 procs"
          },
          {
            "name": "BenchmarkUpdateTargetRevisionOptions/WithHardRefresh - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "606758926 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/FirstTimeCreate",
            "value": 3498669,
            "unit": "ns/op\t 2258598 B/op\t    5508 allocs/op",
            "extra": "339 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/FirstTimeCreate - ns/op",
            "value": 3498669,
            "unit": "ns/op",
            "extra": "339 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/FirstTimeCreate - B/op",
            "value": 2258598,
            "unit": "B/op",
            "extra": "339 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/FirstTimeCreate - allocs/op",
            "value": 5508,
            "unit": "allocs/op",
            "extra": "339 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/UpdateExisting",
            "value": 1972469,
            "unit": "ns/op\t 1167515 B/op\t    3221 allocs/op",
            "extra": "638 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/UpdateExisting - ns/op",
            "value": 1972469,
            "unit": "ns/op",
            "extra": "638 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/UpdateExisting - B/op",
            "value": 1167515,
            "unit": "B/op",
            "extra": "638 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/UpdateExisting - allocs/op",
            "value": 3221,
            "unit": "allocs/op",
            "extra": "638 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/WithAuthentication",
            "value": 3478166,
            "unit": "ns/op\t 2258464 B/op\t    5517 allocs/op",
            "extra": "342 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/WithAuthentication - ns/op",
            "value": 3478166,
            "unit": "ns/op",
            "extra": "342 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/WithAuthentication - B/op",
            "value": 2258464,
            "unit": "B/op",
            "extra": "342 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/WithAuthentication - allocs/op",
            "value": 5517,
            "unit": "allocs/op",
            "extra": "342 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/ProductionConfig",
            "value": 3475753,
            "unit": "ns/op\t 2258705 B/op\t    5521 allocs/op",
            "extra": "345 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/ProductionConfig - ns/op",
            "value": 3475753,
            "unit": "ns/op",
            "extra": "345 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/ProductionConfig - B/op",
            "value": 2258705,
            "unit": "B/op",
            "extra": "345 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerEnsure/ProductionConfig - allocs/op",
            "value": 5521,
            "unit": "allocs/op",
            "extra": "345 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/TargetRevisionOnly",
            "value": 14800,
            "unit": "ns/op\t    9443 B/op\t      76 allocs/op",
            "extra": "79858 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/TargetRevisionOnly - ns/op",
            "value": 14800,
            "unit": "ns/op",
            "extra": "79858 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/TargetRevisionOnly - B/op",
            "value": 9443,
            "unit": "B/op",
            "extra": "79858 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/TargetRevisionOnly - allocs/op",
            "value": 76,
            "unit": "allocs/op",
            "extra": "79858 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/WithHardRefresh",
            "value": 16642,
            "unit": "ns/op\t   11140 B/op\t      87 allocs/op",
            "extra": "71823 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/WithHardRefresh - ns/op",
            "value": 16642,
            "unit": "ns/op",
            "extra": "71823 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/WithHardRefresh - B/op",
            "value": 11140,
            "unit": "B/op",
            "extra": "71823 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/WithHardRefresh - allocs/op",
            "value": 87,
            "unit": "allocs/op",
            "extra": "71823 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/HardRefreshOnly",
            "value": 16646,
            "unit": "ns/op\t   11124 B/op\t      86 allocs/op",
            "extra": "71559 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/HardRefreshOnly - ns/op",
            "value": 16646,
            "unit": "ns/op",
            "extra": "71559 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/HardRefreshOnly - B/op",
            "value": 11124,
            "unit": "B/op",
            "extra": "71559 times\n4 procs"
          },
          {
            "name": "BenchmarkManagerUpdateTargetRevision/HardRefreshOnly - allocs/op",
            "value": 86,
            "unit": "allocs/op",
            "extra": "71559 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager",
            "value": 32.78,
            "unit": "ns/op\t      32 B/op\t       1 allocs/op",
            "extra": "34257870 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager - ns/op",
            "value": 32.78,
            "unit": "ns/op",
            "extra": "34257870 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager - B/op",
            "value": 32,
            "unit": "B/op",
            "extra": "34257870 times\n4 procs"
          },
          {
            "name": "BenchmarkNewManager - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "34257870 times\n4 procs"
          },
          {
            "name": "BenchmarkGetDockerClient",
            "value": 1440,
            "unit": "ns/op\t    1784 B/op\t      23 allocs/op",
            "extra": "769654 times\n4 procs"
          },
          {
            "name": "BenchmarkGetDockerClient - ns/op",
            "value": 1440,
            "unit": "ns/op",
            "extra": "769654 times\n4 procs"
          },
          {
            "name": "BenchmarkGetDockerClient - B/op",
            "value": 1784,
            "unit": "B/op",
            "extra": "769654 times\n4 procs"
          },
          {
            "name": "BenchmarkGetDockerClient - allocs/op",
            "value": 23,
            "unit": "allocs/op",
            "extra": "769654 times\n4 procs"
          },
          {
            "name": "BenchmarkGetConcreteDockerClient",
            "value": 1433,
            "unit": "ns/op\t    1784 B/op\t      23 allocs/op",
            "extra": "793166 times\n4 procs"
          },
          {
            "name": "BenchmarkGetConcreteDockerClient - ns/op",
            "value": 1433,
            "unit": "ns/op",
            "extra": "793166 times\n4 procs"
          },
          {
            "name": "BenchmarkGetConcreteDockerClient - B/op",
            "value": 1784,
            "unit": "B/op",
            "extra": "793166 times\n4 procs"
          },
          {
            "name": "BenchmarkGetConcreteDockerClient - allocs/op",
            "value": 23,
            "unit": "allocs/op",
            "extra": "793166 times\n4 procs"
          },
          {
            "name": "BenchmarkNewRegistryManager",
            "value": 22.33,
            "unit": "ns/op\t      16 B/op\t       1 allocs/op",
            "extra": "53452393 times\n4 procs"
          },
          {
            "name": "BenchmarkNewRegistryManager - ns/op",
            "value": 22.33,
            "unit": "ns/op",
            "extra": "53452393 times\n4 procs"
          },
          {
            "name": "BenchmarkNewRegistryManager - B/op",
            "value": 16,
            "unit": "B/op",
            "extra": "53452393 times\n4 procs"
          },
          {
            "name": "BenchmarkNewRegistryManager - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "53452393 times\n4 procs"
          },
          {
            "name": "BenchmarkNewRegistryManagerNilClient",
            "value": 2.065,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "589022617 times\n4 procs"
          },
          {
            "name": "BenchmarkNewRegistryManagerNilClient - ns/op",
            "value": 2.065,
            "unit": "ns/op",
            "extra": "589022617 times\n4 procs"
          },
          {
            "name": "BenchmarkNewRegistryManagerNilClient - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "589022617 times\n4 procs"
          },
          {
            "name": "BenchmarkNewRegistryManagerNilClient - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "589022617 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildContainerConfig_Minimal",
            "value": 636.3,
            "unit": "ns/op\t    1136 B/op\t      10 allocs/op",
            "extra": "1885671 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildContainerConfig_Minimal - ns/op",
            "value": 636.3,
            "unit": "ns/op",
            "extra": "1885671 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildContainerConfig_Minimal - B/op",
            "value": 1136,
            "unit": "B/op",
            "extra": "1885671 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildContainerConfig_Minimal - allocs/op",
            "value": 10,
            "unit": "allocs/op",
            "extra": "1885671 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildContainerConfig_Production",
            "value": 1072,
            "unit": "ns/op\t    1340 B/op\t      20 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildContainerConfig_Production - ns/op",
            "value": 1072,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildContainerConfig_Production - B/op",
            "value": 1340,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildContainerConfig_Production - allocs/op",
            "value": 20,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildHostConfig_Minimal",
            "value": 328.1,
            "unit": "ns/op\t    1312 B/op\t       3 allocs/op",
            "extra": "3699100 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildHostConfig_Minimal - ns/op",
            "value": 328.1,
            "unit": "ns/op",
            "extra": "3699100 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildHostConfig_Minimal - B/op",
            "value": 1312,
            "unit": "B/op",
            "extra": "3699100 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildHostConfig_Minimal - allocs/op",
            "value": 3,
            "unit": "allocs/op",
            "extra": "3699100 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildNetworkConfig_Minimal",
            "value": 2.642,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "460807176 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildNetworkConfig_Minimal - ns/op",
            "value": 2.642,
            "unit": "ns/op",
            "extra": "460807176 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildNetworkConfig_Minimal - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "460807176 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildNetworkConfig_Minimal - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "460807176 times\n4 procs"
          },
          {
            "name": "BenchmarkResolveVolumeName_Minimal",
            "value": 7.086,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "169241482 times\n4 procs"
          },
          {
            "name": "BenchmarkResolveVolumeName_Minimal - ns/op",
            "value": 7.086,
            "unit": "ns/op",
            "extra": "169241482 times\n4 procs"
          },
          {
            "name": "BenchmarkResolveVolumeName_Minimal - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "169241482 times\n4 procs"
          },
          {
            "name": "BenchmarkResolveVolumeName_Minimal - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "169241482 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildProxyCredentialsEnv_WithCredentials",
            "value": 335.6,
            "unit": "ns/op\t     161 B/op\t       9 allocs/op",
            "extra": "3563330 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildProxyCredentialsEnv_WithCredentials - ns/op",
            "value": 335.6,
            "unit": "ns/op",
            "extra": "3563330 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildProxyCredentialsEnv_WithCredentials - B/op",
            "value": 161,
            "unit": "B/op",
            "extra": "3563330 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildProxyCredentialsEnv_WithCredentials - allocs/op",
            "value": 9,
            "unit": "allocs/op",
            "extra": "3563330 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildProxyCredentialsEnv_NoCredentials",
            "value": 5.492,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "218575443 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildProxyCredentialsEnv_NoCredentials - ns/op",
            "value": 5.492,
            "unit": "ns/op",
            "extra": "218575443 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildProxyCredentialsEnv_NoCredentials - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "218575443 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildProxyCredentialsEnv_NoCredentials - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "218575443 times\n4 procs"
          },
          {
            "name": "BenchmarkClient_CreateCreateCommand",
            "value": 20323,
            "unit": "ns/op\t   29984 B/op\t     170 allocs/op",
            "extra": "58045 times\n4 procs"
          },
          {
            "name": "BenchmarkClient_CreateCreateCommand - ns/op",
            "value": 20323,
            "unit": "ns/op",
            "extra": "58045 times\n4 procs"
          },
          {
            "name": "BenchmarkClient_CreateCreateCommand - B/op",
            "value": 29984,
            "unit": "B/op",
            "extra": "58045 times\n4 procs"
          },
          {
            "name": "BenchmarkClient_CreateCreateCommand - allocs/op",
            "value": 170,
            "unit": "allocs/op",
            "extra": "58045 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/Minimal",
            "value": 29.69,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "39844918 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/Minimal - ns/op",
            "value": 29.69,
            "unit": "ns/op",
            "extra": "39844918 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/Minimal - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "39844918 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/Minimal - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "39844918 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/WithReference",
            "value": 81.26,
            "unit": "ns/op\t      80 B/op\t       1 allocs/op",
            "extra": "14620720 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/WithReference - ns/op",
            "value": 81.26,
            "unit": "ns/op",
            "extra": "14620720 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/WithReference - B/op",
            "value": 80,
            "unit": "B/op",
            "extra": "14620720 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/WithReference - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "14620720 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/Production",
            "value": 431.4,
            "unit": "ns/op\t     440 B/op\t       5 allocs/op",
            "extra": "2773160 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/Production - ns/op",
            "value": 431.4,
            "unit": "ns/op",
            "extra": "2773160 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/Production - B/op",
            "value": 440,
            "unit": "B/op",
            "extra": "2773160 times\n4 procs"
          },
          {
            "name": "BenchmarkGitRepository_Creation/Production - allocs/op",
            "value": 5,
            "unit": "allocs/op",
            "extra": "2773160 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRepository_Creation/Minimal",
            "value": 23.5,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "50523723 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRepository_Creation/Minimal - ns/op",
            "value": 23.5,
            "unit": "ns/op",
            "extra": "50523723 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRepository_Creation/Minimal - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "50523723 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRepository_Creation/Minimal - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "50523723 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRepository_Creation/Production",
            "value": 419.1,
            "unit": "ns/op\t     360 B/op\t       4 allocs/op",
            "extra": "3339486 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRepository_Creation/Production - ns/op",
            "value": 419.1,
            "unit": "ns/op",
            "extra": "3339486 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRepository_Creation/Production - B/op",
            "value": 360,
            "unit": "B/op",
            "extra": "3339486 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRepository_Creation/Production - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "3339486 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/Minimal",
            "value": 25.21,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "46942020 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/Minimal - ns/op",
            "value": 25.21,
            "unit": "ns/op",
            "extra": "46942020 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/Minimal - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "46942020 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/Minimal - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "46942020 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/WithReference",
            "value": 68.47,
            "unit": "ns/op\t      64 B/op\t       1 allocs/op",
            "extra": "17112889 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/WithReference - ns/op",
            "value": 68.47,
            "unit": "ns/op",
            "extra": "17112889 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/WithReference - B/op",
            "value": 64,
            "unit": "B/op",
            "extra": "17112889 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/WithReference - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "17112889 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/Production",
            "value": 402.7,
            "unit": "ns/op\t     424 B/op\t       5 allocs/op",
            "extra": "2965334 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/Production - ns/op",
            "value": 402.7,
            "unit": "ns/op",
            "extra": "2965334 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/Production - B/op",
            "value": 424,
            "unit": "B/op",
            "extra": "2965334 times\n4 procs"
          },
          {
            "name": "BenchmarkOCIRepository_Creation/Production - allocs/op",
            "value": 5,
            "unit": "allocs/op",
            "extra": "2965334 times\n4 procs"
          },
          {
            "name": "BenchmarkKustomization_Creation/Minimal",
            "value": 38.87,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "30754035 times\n4 procs"
          },
          {
            "name": "BenchmarkKustomization_Creation/Minimal - ns/op",
            "value": 38.87,
            "unit": "ns/op",
            "extra": "30754035 times\n4 procs"
          },
          {
            "name": "BenchmarkKustomization_Creation/Minimal - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "30754035 times\n4 procs"
          },
          {
            "name": "BenchmarkKustomization_Creation/Minimal - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "30754035 times\n4 procs"
          },
          {
            "name": "BenchmarkKustomization_Creation/Production",
            "value": 344.1,
            "unit": "ns/op\t     344 B/op\t       3 allocs/op",
            "extra": "3450537 times\n4 procs"
          },
          {
            "name": "BenchmarkKustomization_Creation/Production - ns/op",
            "value": 344.1,
            "unit": "ns/op",
            "extra": "3450537 times\n4 procs"
          },
          {
            "name": "BenchmarkKustomization_Creation/Production - B/op",
            "value": 344,
            "unit": "B/op",
            "extra": "3450537 times\n4 procs"
          },
          {
            "name": "BenchmarkKustomization_Creation/Production - allocs/op",
            "value": 3,
            "unit": "allocs/op",
            "extra": "3450537 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRelease_Creation/Minimal",
            "value": 145.5,
            "unit": "ns/op\t     176 B/op\t       1 allocs/op",
            "extra": "8140612 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRelease_Creation/Minimal - ns/op",
            "value": 145.5,
            "unit": "ns/op",
            "extra": "8140612 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRelease_Creation/Minimal - B/op",
            "value": 176,
            "unit": "B/op",
            "extra": "8140612 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRelease_Creation/Minimal - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "8140612 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRelease_Creation/Production",
            "value": 585.1,
            "unit": "ns/op\t     672 B/op\t       7 allocs/op",
            "extra": "2054676 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRelease_Creation/Production - ns/op",
            "value": 585.1,
            "unit": "ns/op",
            "extra": "2054676 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRelease_Creation/Production - B/op",
            "value": 672,
            "unit": "B/op",
            "extra": "2054676 times\n4 procs"
          },
          {
            "name": "BenchmarkHelmRelease_Creation/Production - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "2054676 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/GitRepository",
            "value": 473.9,
            "unit": "ns/op\t    1280 B/op\t       2 allocs/op",
            "extra": "2531364 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/GitRepository - ns/op",
            "value": 473.9,
            "unit": "ns/op",
            "extra": "2531364 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/GitRepository - B/op",
            "value": 1280,
            "unit": "B/op",
            "extra": "2531364 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/GitRepository - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "2531364 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/HelmRepository",
            "value": 351.1,
            "unit": "ns/op\t     896 B/op\t       2 allocs/op",
            "extra": "3395953 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/HelmRepository - ns/op",
            "value": 351.1,
            "unit": "ns/op",
            "extra": "3395953 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/HelmRepository - B/op",
            "value": 896,
            "unit": "B/op",
            "extra": "3395953 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/HelmRepository - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "3395953 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/OCIRepository",
            "value": 368,
            "unit": "ns/op\t     960 B/op\t       2 allocs/op",
            "extra": "3306315 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/OCIRepository - ns/op",
            "value": 368,
            "unit": "ns/op",
            "extra": "3306315 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/OCIRepository - B/op",
            "value": 960,
            "unit": "B/op",
            "extra": "3306315 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/OCIRepository - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "3306315 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/Kustomization",
            "value": 619.3,
            "unit": "ns/op\t    1792 B/op\t       2 allocs/op",
            "extra": "1944627 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/Kustomization - ns/op",
            "value": 619.3,
            "unit": "ns/op",
            "extra": "1944627 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/Kustomization - B/op",
            "value": 1792,
            "unit": "B/op",
            "extra": "1944627 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/Kustomization - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "1944627 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/HelmRelease",
            "value": 693.8,
            "unit": "ns/op\t    1968 B/op\t       3 allocs/op",
            "extra": "1718667 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/HelmRelease - ns/op",
            "value": 693.8,
            "unit": "ns/op",
            "extra": "1718667 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/HelmRelease - B/op",
            "value": 1968,
            "unit": "B/op",
            "extra": "1718667 times\n4 procs"
          },
          {
            "name": "BenchmarkCopySpec/HelmRelease - allocs/op",
            "value": 3,
            "unit": "allocs/op",
            "extra": "1718667 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpec/Basic",
            "value": 1.888,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "643833832 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpec/Basic - ns/op",
            "value": 1.888,
            "unit": "ns/op",
            "extra": "643833832 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpec/Basic - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "643833832 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpec/Basic - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "643833832 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpec/WithAllFields",
            "value": 86.41,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "13946390 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpec/WithAllFields - ns/op",
            "value": 86.41,
            "unit": "ns/op",
            "extra": "13946390 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpec/WithAllFields - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "13946390 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpec/WithAllFields - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "13946390 times\n4 procs"
          },
          {
            "name": "BenchmarkRepositoryEntry/Basic",
            "value": 1.883,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "638847368 times\n4 procs"
          },
          {
            "name": "BenchmarkRepositoryEntry/Basic - ns/op",
            "value": 1.883,
            "unit": "ns/op",
            "extra": "638847368 times\n4 procs"
          },
          {
            "name": "BenchmarkRepositoryEntry/Basic - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "638847368 times\n4 procs"
          },
          {
            "name": "BenchmarkRepositoryEntry/Basic - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "638847368 times\n4 procs"
          },
          {
            "name": "BenchmarkRepositoryEntry/WithAuth",
            "value": 1.873,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "651459966 times\n4 procs"
          },
          {
            "name": "BenchmarkRepositoryEntry/WithAuth - ns/op",
            "value": 1.873,
            "unit": "ns/op",
            "extra": "651459966 times\n4 procs"
          },
          {
            "name": "BenchmarkRepositoryEntry/WithAuth - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "651459966 times\n4 procs"
          },
          {
            "name": "BenchmarkRepositoryEntry/WithAuth - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "651459966 times\n4 procs"
          },
          {
            "name": "BenchmarkReleaseInfo",
            "value": 2.134,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "576313182 times\n4 procs"
          },
          {
            "name": "BenchmarkReleaseInfo - ns/op",
            "value": 2.134,
            "unit": "ns/op",
            "extra": "576313182 times\n4 procs"
          },
          {
            "name": "BenchmarkReleaseInfo - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "576313182 times\n4 procs"
          },
          {
            "name": "BenchmarkReleaseInfo - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "576313182 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpecWithLargeValues",
            "value": 1.897,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "607502922 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpecWithLargeValues - ns/op",
            "value": 1.897,
            "unit": "ns/op",
            "extra": "607502922 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpecWithLargeValues - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "607502922 times\n4 procs"
          },
          {
            "name": "BenchmarkChartSpecWithLargeValues - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "607502922 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateClient",
            "value": 38.64,
            "unit": "ns/op\t      48 B/op\t       1 allocs/op",
            "extra": "30221990 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateClient - ns/op",
            "value": 38.64,
            "unit": "ns/op",
            "extra": "30221990 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateClient - B/op",
            "value": 48,
            "unit": "B/op",
            "extra": "30221990 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateClient - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "30221990 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateApplyCommand",
            "value": 48609,
            "unit": "ns/op\t   61917 B/op\t     311 allocs/op",
            "extra": "27127 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateApplyCommand - ns/op",
            "value": 48609,
            "unit": "ns/op",
            "extra": "27127 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateApplyCommand - B/op",
            "value": 61917,
            "unit": "B/op",
            "extra": "27127 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateApplyCommand - allocs/op",
            "value": 311,
            "unit": "allocs/op",
            "extra": "27127 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateGetCommand",
            "value": 33036,
            "unit": "ns/op\t   44446 B/op\t     205 allocs/op",
            "extra": "54957 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateGetCommand - ns/op",
            "value": 33036,
            "unit": "ns/op",
            "extra": "54957 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateGetCommand - B/op",
            "value": 44446,
            "unit": "B/op",
            "extra": "54957 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateGetCommand - allocs/op",
            "value": 205,
            "unit": "allocs/op",
            "extra": "54957 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDeleteCommand",
            "value": 19405,
            "unit": "ns/op\t   27382 B/op\t     121 allocs/op",
            "extra": "102328 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDeleteCommand - ns/op",
            "value": 19405,
            "unit": "ns/op",
            "extra": "102328 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDeleteCommand - B/op",
            "value": 27382,
            "unit": "B/op",
            "extra": "102328 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDeleteCommand - allocs/op",
            "value": 121,
            "unit": "allocs/op",
            "extra": "102328 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDescribeCommand",
            "value": 20968,
            "unit": "ns/op\t   30120 B/op\t     142 allocs/op",
            "extra": "98623 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDescribeCommand - ns/op",
            "value": 20968,
            "unit": "ns/op",
            "extra": "98623 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDescribeCommand - B/op",
            "value": 30120,
            "unit": "B/op",
            "extra": "98623 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDescribeCommand - allocs/op",
            "value": 142,
            "unit": "allocs/op",
            "extra": "98623 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateLogsCommand",
            "value": 28851,
            "unit": "ns/op\t   31656 B/op\t     144 allocs/op",
            "extra": "67342 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateLogsCommand - ns/op",
            "value": 28851,
            "unit": "ns/op",
            "extra": "67342 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateLogsCommand - B/op",
            "value": 31656,
            "unit": "B/op",
            "extra": "67342 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateLogsCommand - allocs/op",
            "value": 144,
            "unit": "allocs/op",
            "extra": "67342 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateWaitCommand",
            "value": 10589,
            "unit": "ns/op\t   12768 B/op\t      92 allocs/op",
            "extra": "160426 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateWaitCommand - ns/op",
            "value": 10589,
            "unit": "ns/op",
            "extra": "160426 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateWaitCommand - B/op",
            "value": 12768,
            "unit": "B/op",
            "extra": "160426 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateWaitCommand - allocs/op",
            "value": 92,
            "unit": "allocs/op",
            "extra": "160426 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateNamespaceCmd",
            "value": 259530,
            "unit": "ns/op\t  280872 B/op\t    1561 allocs/op",
            "extra": "8056 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateNamespaceCmd - ns/op",
            "value": 259530,
            "unit": "ns/op",
            "extra": "8056 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateNamespaceCmd - B/op",
            "value": 280872,
            "unit": "B/op",
            "extra": "8056 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateNamespaceCmd - allocs/op",
            "value": 1561,
            "unit": "allocs/op",
            "extra": "8056 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDeploymentCmd",
            "value": 268992,
            "unit": "ns/op\t  281536 B/op\t    1565 allocs/op",
            "extra": "6532 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDeploymentCmd - ns/op",
            "value": 268992,
            "unit": "ns/op",
            "extra": "6532 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDeploymentCmd - B/op",
            "value": 281536,
            "unit": "B/op",
            "extra": "6532 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateDeploymentCmd - allocs/op",
            "value": 1565,
            "unit": "allocs/op",
            "extra": "6532 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateServiceCmd",
            "value": 187248,
            "unit": "ns/op\t  290048 B/op\t    1631 allocs/op",
            "extra": "6315 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateServiceCmd - ns/op",
            "value": 187248,
            "unit": "ns/op",
            "extra": "6315 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateServiceCmd - B/op",
            "value": 290048,
            "unit": "B/op",
            "extra": "6315 times\n4 procs"
          },
          {
            "name": "BenchmarkCreateServiceCmd - allocs/op",
            "value": 1631,
            "unit": "allocs/op",
            "extra": "6315 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_SmallKustomization",
            "value": 284390,
            "unit": "ns/op\t  212948 B/op\t    1609 allocs/op",
            "extra": "3825 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_SmallKustomization - ns/op",
            "value": 284390,
            "unit": "ns/op",
            "extra": "3825 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_SmallKustomization - B/op",
            "value": 212948,
            "unit": "B/op",
            "extra": "3825 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_SmallKustomization - allocs/op",
            "value": 1609,
            "unit": "allocs/op",
            "extra": "3825 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_MediumKustomization",
            "value": 962880,
            "unit": "ns/op\t  711007 B/op\t    6076 allocs/op",
            "extra": "1111 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_MediumKustomization - ns/op",
            "value": 962880,
            "unit": "ns/op",
            "extra": "1111 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_MediumKustomization - B/op",
            "value": 711007,
            "unit": "B/op",
            "extra": "1111 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_MediumKustomization - allocs/op",
            "value": 6076,
            "unit": "allocs/op",
            "extra": "1111 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_WithLabels",
            "value": 1592320,
            "unit": "ns/op\t 1151736 B/op\t   10296 allocs/op",
            "extra": "754 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_WithLabels - ns/op",
            "value": 1592320,
            "unit": "ns/op",
            "extra": "754 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_WithLabels - B/op",
            "value": 1151736,
            "unit": "B/op",
            "extra": "754 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_WithLabels - allocs/op",
            "value": 10296,
            "unit": "allocs/op",
            "extra": "754 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_LargeKustomization",
            "value": 2729672,
            "unit": "ns/op\t 2284816 B/op\t   18281 allocs/op",
            "extra": "438 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_LargeKustomization - ns/op",
            "value": 2729672,
            "unit": "ns/op",
            "extra": "438 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_LargeKustomization - B/op",
            "value": 2284816,
            "unit": "B/op",
            "extra": "438 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_LargeKustomization - allocs/op",
            "value": 18281,
            "unit": "allocs/op",
            "extra": "438 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_WithNamePrefix",
            "value": 583398,
            "unit": "ns/op\t  477601 B/op\t    4400 allocs/op",
            "extra": "2062 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_WithNamePrefix - ns/op",
            "value": 583398,
            "unit": "ns/op",
            "extra": "2062 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_WithNamePrefix - B/op",
            "value": 477601,
            "unit": "B/op",
            "extra": "2062 times\n4 procs"
          },
          {
            "name": "BenchmarkBuild_WithNamePrefix - allocs/op",
            "value": 4400,
            "unit": "allocs/op",
            "extra": "2062 times\n4 procs"
          },
          {
            "name": "BenchmarkInitializeViper",
            "value": 14497,
            "unit": "ns/op\t    6576 B/op\t      77 allocs/op",
            "extra": "79435 times\n4 procs"
          },
          {
            "name": "BenchmarkInitializeViper - ns/op",
            "value": 14497,
            "unit": "ns/op",
            "extra": "79435 times\n4 procs"
          },
          {
            "name": "BenchmarkInitializeViper - B/op",
            "value": 6576,
            "unit": "B/op",
            "extra": "79435 times\n4 procs"
          },
          {
            "name": "BenchmarkInitializeViper - allocs/op",
            "value": 77,
            "unit": "allocs/op",
            "extra": "79435 times\n4 procs"
          },
          {
            "name": "BenchmarkNewConfigManager_WithSelectors",
            "value": 14380,
            "unit": "ns/op\t    6576 B/op\t      77 allocs/op",
            "extra": "83343 times\n4 procs"
          },
          {
            "name": "BenchmarkNewConfigManager_WithSelectors - ns/op",
            "value": 14380,
            "unit": "ns/op",
            "extra": "83343 times\n4 procs"
          },
          {
            "name": "BenchmarkNewConfigManager_WithSelectors - B/op",
            "value": 6576,
            "unit": "B/op",
            "extra": "83343 times\n4 procs"
          },
          {
            "name": "BenchmarkNewConfigManager_WithSelectors - allocs/op",
            "value": 77,
            "unit": "allocs/op",
            "extra": "83343 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_NoConfigFile",
            "value": 55234,
            "unit": "ns/op\t   21825 B/op\t     463 allocs/op",
            "extra": "21675 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_NoConfigFile - ns/op",
            "value": 55234,
            "unit": "ns/op",
            "extra": "21675 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_NoConfigFile - B/op",
            "value": 21825,
            "unit": "B/op",
            "extra": "21675 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_NoConfigFile - allocs/op",
            "value": 463,
            "unit": "allocs/op",
            "extra": "21675 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_WithConfigFile",
            "value": 160155,
            "unit": "ns/op\t   70918 B/op\t    1070 allocs/op",
            "extra": "8166 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_WithConfigFile - ns/op",
            "value": 160155,
            "unit": "ns/op",
            "extra": "8166 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_WithConfigFile - B/op",
            "value": 70918,
            "unit": "B/op",
            "extra": "8166 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_WithConfigFile - allocs/op",
            "value": 1070,
            "unit": "allocs/op",
            "extra": "8166 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_WithConfigFile_DeepTree",
            "value": 145640,
            "unit": "ns/op\t   63304 B/op\t     971 allocs/op",
            "extra": "8913 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_WithConfigFile_DeepTree - ns/op",
            "value": 145640,
            "unit": "ns/op",
            "extra": "8913 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_WithConfigFile_DeepTree - B/op",
            "value": 63304,
            "unit": "B/op",
            "extra": "8913 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_WithConfigFile_DeepTree - allocs/op",
            "value": 971,
            "unit": "allocs/op",
            "extra": "8913 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_Cached",
            "value": 2.915,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "404872238 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_Cached - ns/op",
            "value": 2.915,
            "unit": "ns/op",
            "extra": "404872238 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_Cached - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "404872238 times\n4 procs"
          },
          {
            "name": "BenchmarkLoad_Cached - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "404872238 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Simple",
            "value": 12405,
            "unit": "ns/op\t   14771 B/op\t      81 allocs/op",
            "extra": "85182 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Simple - ns/op",
            "value": 12405,
            "unit": "ns/op",
            "extra": "85182 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Simple - B/op",
            "value": 14771,
            "unit": "B/op",
            "extra": "85182 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Simple - allocs/op",
            "value": 81,
            "unit": "allocs/op",
            "extra": "85182 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/nested",
            "value": 25039,
            "unit": "ns/op\t   28967 B/op\t     149 allocs/op",
            "extra": "47163 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/nested - ns/op",
            "value": 25039,
            "unit": "ns/op",
            "extra": "47163 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/nested - B/op",
            "value": 28967,
            "unit": "B/op",
            "extra": "47163 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/nested - allocs/op",
            "value": 149,
            "unit": "allocs/op",
            "extra": "47163 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/slice",
            "value": 32572,
            "unit": "ns/op\t   43301 B/op\t     234 allocs/op",
            "extra": "36675 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/slice - ns/op",
            "value": 32572,
            "unit": "ns/op",
            "extra": "36675 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/slice - B/op",
            "value": 43301,
            "unit": "B/op",
            "extra": "36675 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/slice - allocs/op",
            "value": 234,
            "unit": "allocs/op",
            "extra": "36675 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/map",
            "value": 22448,
            "unit": "ns/op\t   29831 B/op\t     175 allocs/op",
            "extra": "53124 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/map - ns/op",
            "value": 22448,
            "unit": "ns/op",
            "extra": "53124 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/map - B/op",
            "value": 29831,
            "unit": "B/op",
            "extra": "53124 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/map - allocs/op",
            "value": 175,
            "unit": "allocs/op",
            "extra": "53124 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/large-slice",
            "value": 580402,
            "unit": "ns/op\t  710665 B/op\t    3934 allocs/op",
            "extra": "2062 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/large-slice - ns/op",
            "value": 580402,
            "unit": "ns/op",
            "extra": "2062 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/large-slice - B/op",
            "value": 710665,
            "unit": "B/op",
            "extra": "2062 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Marshal_Nested/large-slice - allocs/op",
            "value": 3934,
            "unit": "allocs/op",
            "extra": "2062 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Simple",
            "value": 7453,
            "unit": "ns/op\t    7505 B/op\t      73 allocs/op",
            "extra": "162385 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Simple - ns/op",
            "value": 7453,
            "unit": "ns/op",
            "extra": "162385 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Simple - B/op",
            "value": 7505,
            "unit": "B/op",
            "extra": "162385 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Simple - allocs/op",
            "value": 73,
            "unit": "allocs/op",
            "extra": "162385 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/nested",
            "value": 11472,
            "unit": "ns/op\t    9377 B/op\t     114 allocs/op",
            "extra": "103179 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/nested - ns/op",
            "value": 11472,
            "unit": "ns/op",
            "extra": "103179 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/nested - B/op",
            "value": 9377,
            "unit": "B/op",
            "extra": "103179 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/nested - allocs/op",
            "value": 114,
            "unit": "allocs/op",
            "extra": "103179 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/slice",
            "value": 22119,
            "unit": "ns/op\t   13755 B/op\t     208 allocs/op",
            "extra": "54238 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/slice - ns/op",
            "value": 22119,
            "unit": "ns/op",
            "extra": "54238 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/slice - B/op",
            "value": 13755,
            "unit": "B/op",
            "extra": "54238 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/slice - allocs/op",
            "value": 208,
            "unit": "allocs/op",
            "extra": "54238 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/map",
            "value": 13357,
            "unit": "ns/op\t   10290 B/op\t     137 allocs/op",
            "extra": "89564 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/map - ns/op",
            "value": 13357,
            "unit": "ns/op",
            "extra": "89564 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/map - B/op",
            "value": 10290,
            "unit": "B/op",
            "extra": "89564 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/map - allocs/op",
            "value": 137,
            "unit": "allocs/op",
            "extra": "89564 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/large-slice",
            "value": 442292,
            "unit": "ns/op\t  208759 B/op\t    3906 allocs/op",
            "extra": "2720 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/large-slice - ns/op",
            "value": 442292,
            "unit": "ns/op",
            "extra": "2720 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/large-slice - B/op",
            "value": 208759,
            "unit": "B/op",
            "extra": "2720 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_Unmarshal_Nested/large-slice - allocs/op",
            "value": 3906,
            "unit": "allocs/op",
            "extra": "2720 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/simple",
            "value": 7773,
            "unit": "ns/op\t    7529 B/op\t      74 allocs/op",
            "extra": "156374 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/simple - ns/op",
            "value": 7773,
            "unit": "ns/op",
            "extra": "156374 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/simple - B/op",
            "value": 7529,
            "unit": "B/op",
            "extra": "156374 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/simple - allocs/op",
            "value": 74,
            "unit": "allocs/op",
            "extra": "156374 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/multiline",
            "value": 7930,
            "unit": "ns/op\t    7609 B/op\t      74 allocs/op",
            "extra": "150330 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/multiline - ns/op",
            "value": 7930,
            "unit": "ns/op",
            "extra": "150330 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/multiline - B/op",
            "value": 7609,
            "unit": "B/op",
            "extra": "150330 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/multiline - allocs/op",
            "value": 74,
            "unit": "allocs/op",
            "extra": "150330 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/whitespace",
            "value": 7618,
            "unit": "ns/op\t    7553 B/op\t      76 allocs/op",
            "extra": "157002 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/whitespace - ns/op",
            "value": 7618,
            "unit": "ns/op",
            "extra": "157002 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/whitespace - B/op",
            "value": 7553,
            "unit": "B/op",
            "extra": "157002 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_UnmarshalString/whitespace - allocs/op",
            "value": 76,
            "unit": "allocs/op",
            "extra": "157002 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/simple",
            "value": 18041,
            "unit": "ns/op\t   22292 B/op\t     155 allocs/op",
            "extra": "65764 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/simple - ns/op",
            "value": 18041,
            "unit": "ns/op",
            "extra": "65764 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/simple - B/op",
            "value": 22292,
            "unit": "B/op",
            "extra": "65764 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/simple - allocs/op",
            "value": 155,
            "unit": "allocs/op",
            "extra": "65764 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/empty",
            "value": 17440,
            "unit": "ns/op\t   22124 B/op\t     143 allocs/op",
            "extra": "68854 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/empty - ns/op",
            "value": 17440,
            "unit": "ns/op",
            "extra": "68854 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/empty - B/op",
            "value": 22124,
            "unit": "B/op",
            "extra": "68854 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/empty - allocs/op",
            "value": 143,
            "unit": "allocs/op",
            "extra": "68854 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/large-value",
            "value": 18830,
            "unit": "ns/op\t   22404 B/op\t     158 allocs/op",
            "extra": "64021 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/large-value - ns/op",
            "value": 18830,
            "unit": "ns/op",
            "extra": "64021 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/large-value - B/op",
            "value": 22404,
            "unit": "B/op",
            "extra": "64021 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip/large-value - allocs/op",
            "value": 158,
            "unit": "allocs/op",
            "extra": "64021 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip_Nested",
            "value": 81293,
            "unit": "ns/op\t   98838 B/op\t     611 allocs/op",
            "extra": "14800 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip_Nested - ns/op",
            "value": 81293,
            "unit": "ns/op",
            "extra": "14800 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip_Nested - B/op",
            "value": 98838,
            "unit": "B/op",
            "extra": "14800 times\n4 procs"
          },
          {
            "name": "BenchmarkYAMLMarshaller_RoundTrip_Nested - allocs/op",
            "value": 611,
            "unit": "allocs/op",
            "extra": "14800 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/1_resource",
            "value": 4466,
            "unit": "ns/op\t    4899 B/op\t      32 allocs/op",
            "extra": "248122 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/1_resource - ns/op",
            "value": 4466,
            "unit": "ns/op",
            "extra": "248122 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/1_resource - B/op",
            "value": 4899,
            "unit": "B/op",
            "extra": "248122 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/1_resource - allocs/op",
            "value": 32,
            "unit": "allocs/op",
            "extra": "248122 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/5_resources",
            "value": 23928,
            "unit": "ns/op\t   24468 B/op\t     160 allocs/op",
            "extra": "52915 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/5_resources - ns/op",
            "value": 23928,
            "unit": "ns/op",
            "extra": "52915 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/5_resources - B/op",
            "value": 24468,
            "unit": "B/op",
            "extra": "52915 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/5_resources - allocs/op",
            "value": 160,
            "unit": "allocs/op",
            "extra": "52915 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/10_resources",
            "value": 49214,
            "unit": "ns/op\t   48904 B/op\t     320 allocs/op",
            "extra": "21834 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/10_resources - ns/op",
            "value": 49214,
            "unit": "ns/op",
            "extra": "21834 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/10_resources - B/op",
            "value": 48904,
            "unit": "B/op",
            "extra": "21834 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/10_resources - allocs/op",
            "value": 320,
            "unit": "allocs/op",
            "extra": "21834 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/20_resources",
            "value": 89268,
            "unit": "ns/op\t   97855 B/op\t     640 allocs/op",
            "extra": "13364 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/20_resources - ns/op",
            "value": 89268,
            "unit": "ns/op",
            "extra": "13364 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/20_resources - B/op",
            "value": 97855,
            "unit": "B/op",
            "extra": "13364 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_Sequential/20_resources - allocs/op",
            "value": 640,
            "unit": "allocs/op",
            "extra": "13364 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/2d_2ds",
            "value": 18004,
            "unit": "ns/op\t   19584 B/op\t     128 allocs/op",
            "extra": "64264 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/2d_2ds - ns/op",
            "value": 18004,
            "unit": "ns/op",
            "extra": "64264 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/2d_2ds - B/op",
            "value": 19584,
            "unit": "B/op",
            "extra": "64264 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/2d_2ds - allocs/op",
            "value": 128,
            "unit": "allocs/op",
            "extra": "64264 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/5d_5ds",
            "value": 44883,
            "unit": "ns/op\t   48932 B/op\t     320 allocs/op",
            "extra": "26582 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/5d_5ds - ns/op",
            "value": 44883,
            "unit": "ns/op",
            "extra": "26582 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/5d_5ds - B/op",
            "value": 48932,
            "unit": "B/op",
            "extra": "26582 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/5d_5ds - allocs/op",
            "value": 320,
            "unit": "allocs/op",
            "extra": "26582 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/10d_10ds",
            "value": 89362,
            "unit": "ns/op\t   97846 B/op\t     640 allocs/op",
            "extra": "13437 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/10d_10ds - ns/op",
            "value": 89362,
            "unit": "ns/op",
            "extra": "13437 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/10d_10ds - B/op",
            "value": 97846,
            "unit": "B/op",
            "extra": "13437 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_MixedTypes/10d_10ds - allocs/op",
            "value": 640,
            "unit": "allocs/op",
            "extra": "13437 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_RealWorldCNI",
            "value": 13363,
            "unit": "ns/op\t   14686 B/op\t      96 allocs/op",
            "extra": "86398 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_RealWorldCNI - ns/op",
            "value": 13363,
            "unit": "ns/op",
            "extra": "86398 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_RealWorldCNI - B/op",
            "value": 14686,
            "unit": "B/op",
            "extra": "86398 times\n4 procs"
          },
          {
            "name": "BenchmarkWaitForMultipleResources_RealWorldCNI - allocs/op",
            "value": 96,
            "unit": "allocs/op",
            "extra": "86398 times\n4 procs"
          },
          {
            "name": "BenchmarkPollForReadiness_SingleCheck",
            "value": 1025,
            "unit": "ns/op\t     688 B/op\t      11 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkPollForReadiness_SingleCheck - ns/op",
            "value": 1025,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkPollForReadiness_SingleCheck - B/op",
            "value": 688,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkPollForReadiness_SingleCheck - allocs/op",
            "value": 11,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_SingleLine",
            "value": 124.9,
            "unit": "ns/op\t      32 B/op\t       2 allocs/op",
            "extra": "9516758 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_SingleLine - ns/op",
            "value": 124.9,
            "unit": "ns/op",
            "extra": "9516758 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_SingleLine - B/op",
            "value": 32,
            "unit": "B/op",
            "extra": "9516758 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_SingleLine - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "9516758 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_WithArgs",
            "value": 324.4,
            "unit": "ns/op\t     112 B/op\t       4 allocs/op",
            "extra": "3735028 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_WithArgs - ns/op",
            "value": 324.4,
            "unit": "ns/op",
            "extra": "3735028 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_WithArgs - B/op",
            "value": 112,
            "unit": "B/op",
            "extra": "3735028 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_WithArgs - allocs/op",
            "value": 4,
            "unit": "allocs/op",
            "extra": "3735028 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_Multiline",
            "value": 428.7,
            "unit": "ns/op\t     208 B/op\t       7 allocs/op",
            "extra": "2573196 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_Multiline - ns/op",
            "value": 428.7,
            "unit": "ns/op",
            "extra": "2573196 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_Multiline - B/op",
            "value": 208,
            "unit": "B/op",
            "extra": "2573196 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_Multiline - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "2573196 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_WithTimer",
            "value": 378.3,
            "unit": "ns/op\t      68 B/op\t       6 allocs/op",
            "extra": "3173608 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_WithTimer - ns/op",
            "value": 378.3,
            "unit": "ns/op",
            "extra": "3173608 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_WithTimer - B/op",
            "value": 68,
            "unit": "B/op",
            "extra": "3173608 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_WithTimer - allocs/op",
            "value": 6,
            "unit": "allocs/op",
            "extra": "3173608 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_AllTypes",
            "value": 1027,
            "unit": "ns/op\t     224 B/op\t      14 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_AllTypes - ns/op",
            "value": 1027,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_AllTypes - B/op",
            "value": 224,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkWriteMessage_AllTypes - allocs/op",
            "value": 14,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Sequential",
            "value": 43845,
            "unit": "ns/op\t    2104 B/op\t      34 allocs/op",
            "extra": "27798 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Sequential - ns/op",
            "value": 43845,
            "unit": "ns/op",
            "extra": "27798 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Sequential - B/op",
            "value": 2104,
            "unit": "B/op",
            "extra": "27798 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Sequential - allocs/op",
            "value": 34,
            "unit": "allocs/op",
            "extra": "27798 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Parallel_Fast",
            "value": 27658,
            "unit": "ns/op\t    3538 B/op\t      80 allocs/op",
            "extra": "43165 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Parallel_Fast - ns/op",
            "value": 27658,
            "unit": "ns/op",
            "extra": "43165 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Parallel_Fast - B/op",
            "value": 3538,
            "unit": "B/op",
            "extra": "43165 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Parallel_Fast - allocs/op",
            "value": 80,
            "unit": "allocs/op",
            "extra": "43165 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Parallel_Slow",
            "value": 117859,
            "unit": "ns/op\t    2784 B/op\t      58 allocs/op",
            "extra": "10000 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Parallel_Slow - ns/op",
            "value": 117859,
            "unit": "ns/op",
            "extra": "10000 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Parallel_Slow - B/op",
            "value": 2784,
            "unit": "B/op",
            "extra": "10000 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_Parallel_Slow - allocs/op",
            "value": 58,
            "unit": "allocs/op",
            "extra": "10000 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_ManyTasks",
            "value": 178161,
            "unit": "ns/op\t   13597 B/op\t     249 allocs/op",
            "extra": "6688 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_ManyTasks - ns/op",
            "value": 178161,
            "unit": "ns/op",
            "extra": "6688 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_ManyTasks - B/op",
            "value": 13597,
            "unit": "B/op",
            "extra": "6688 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_ManyTasks - allocs/op",
            "value": 249,
            "unit": "allocs/op",
            "extra": "6688 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_WithTimer",
            "value": 34621,
            "unit": "ns/op\t    3009 B/op\t      71 allocs/op",
            "extra": "34712 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_WithTimer - ns/op",
            "value": 34621,
            "unit": "ns/op",
            "extra": "34712 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_WithTimer - B/op",
            "value": 3009,
            "unit": "B/op",
            "extra": "34712 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_WithTimer - allocs/op",
            "value": 71,
            "unit": "allocs/op",
            "extra": "34712 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_CI_Mode",
            "value": 33281,
            "unit": "ns/op\t    2784 B/op\t      58 allocs/op",
            "extra": "36212 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_CI_Mode - ns/op",
            "value": 33281,
            "unit": "ns/op",
            "extra": "36212 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_CI_Mode - B/op",
            "value": 2784,
            "unit": "B/op",
            "extra": "36212 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_CI_Mode - allocs/op",
            "value": 58,
            "unit": "allocs/op",
            "extra": "36212 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_CustomLabels",
            "value": 25159,
            "unit": "ns/op\t    2409 B/op\t      46 allocs/op",
            "extra": "47550 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_CustomLabels - ns/op",
            "value": 25159,
            "unit": "ns/op",
            "extra": "47550 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_CustomLabels - B/op",
            "value": 2409,
            "unit": "B/op",
            "extra": "47550 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_CustomLabels - allocs/op",
            "value": 46,
            "unit": "allocs/op",
            "extra": "47550 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_SingleTask",
            "value": 14782,
            "unit": "ns/op\t    2096 B/op\t      34 allocs/op",
            "extra": "84537 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_SingleTask - ns/op",
            "value": 14782,
            "unit": "ns/op",
            "extra": "84537 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_SingleTask - B/op",
            "value": 2096,
            "unit": "B/op",
            "extra": "84537 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_SingleTask - allocs/op",
            "value": 34,
            "unit": "allocs/op",
            "extra": "84537 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_NoOp",
            "value": 15877,
            "unit": "ns/op\t    2784 B/op\t      58 allocs/op",
            "extra": "74935 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_NoOp - ns/op",
            "value": 15877,
            "unit": "ns/op",
            "extra": "74935 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_NoOp - B/op",
            "value": 2784,
            "unit": "B/op",
            "extra": "74935 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_NoOp - allocs/op",
            "value": 58,
            "unit": "allocs/op",
            "extra": "74935 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_VaryingTaskDurations",
            "value": 87211,
            "unit": "ns/op\t    3033 B/op\t      68 allocs/op",
            "extra": "13638 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_VaryingTaskDurations - ns/op",
            "value": 87211,
            "unit": "ns/op",
            "extra": "13638 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_VaryingTaskDurations - B/op",
            "value": 3033,
            "unit": "B/op",
            "extra": "13638 times\n4 procs"
          },
          {
            "name": "BenchmarkProgressGroup_VaryingTaskDurations - allocs/op",
            "value": 68,
            "unit": "allocs/op",
            "extra": "13638 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_NoChanges",
            "value": 350.1,
            "unit": "ns/op\t     832 B/op\t       5 allocs/op",
            "extra": "3422379 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_NoChanges - ns/op",
            "value": 350.1,
            "unit": "ns/op",
            "extra": "3422379 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_NoChanges - B/op",
            "value": 832,
            "unit": "B/op",
            "extra": "3422379 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_NoChanges - allocs/op",
            "value": 5,
            "unit": "allocs/op",
            "extra": "3422379 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_AllInPlaceChanges",
            "value": 833.2,
            "unit": "ns/op\t    1984 B/op\t       9 allocs/op",
            "extra": "1542456 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_AllInPlaceChanges - ns/op",
            "value": 833.2,
            "unit": "ns/op",
            "extra": "1542456 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_AllInPlaceChanges - B/op",
            "value": 1984,
            "unit": "B/op",
            "extra": "1542456 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_AllInPlaceChanges - allocs/op",
            "value": 9,
            "unit": "allocs/op",
            "extra": "1542456 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_RecreateRequired",
            "value": 397,
            "unit": "ns/op\t     912 B/op\t       6 allocs/op",
            "extra": "3030410 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_RecreateRequired - ns/op",
            "value": 397,
            "unit": "ns/op",
            "extra": "3030410 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_RecreateRequired - B/op",
            "value": 912,
            "unit": "B/op",
            "extra": "3030410 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_RecreateRequired - allocs/op",
            "value": 6,
            "unit": "allocs/op",
            "extra": "3030410 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_MixedCategories",
            "value": 584.3,
            "unit": "ns/op\t    1280 B/op\t       9 allocs/op",
            "extra": "2057852 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_MixedCategories - ns/op",
            "value": 584.3,
            "unit": "ns/op",
            "extra": "2057852 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_MixedCategories - B/op",
            "value": 1280,
            "unit": "B/op",
            "extra": "2057852 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_MixedCategories - allocs/op",
            "value": 9,
            "unit": "allocs/op",
            "extra": "2057852 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_TalosOptions",
            "value": 688.5,
            "unit": "ns/op\t    1360 B/op\t      10 allocs/op",
            "extra": "1748502 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_TalosOptions - ns/op",
            "value": 688.5,
            "unit": "ns/op",
            "extra": "1748502 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_TalosOptions - B/op",
            "value": 1360,
            "unit": "B/op",
            "extra": "1748502 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_TalosOptions - allocs/op",
            "value": 10,
            "unit": "allocs/op",
            "extra": "1748502 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_HetznerOptions",
            "value": 757.5,
            "unit": "ns/op\t    1456 B/op\t      10 allocs/op",
            "extra": "1583174 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_HetznerOptions - ns/op",
            "value": 757.5,
            "unit": "ns/op",
            "extra": "1583174 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_HetznerOptions - B/op",
            "value": 1456,
            "unit": "B/op",
            "extra": "1583174 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_HetznerOptions - allocs/op",
            "value": 10,
            "unit": "allocs/op",
            "extra": "1583174 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_NilSpec",
            "value": 49.98,
            "unit": "ns/op\t     144 B/op\t       1 allocs/op",
            "extra": "24385216 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_NilSpec - ns/op",
            "value": 49.98,
            "unit": "ns/op",
            "extra": "24385216 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_NilSpec - B/op",
            "value": 144,
            "unit": "B/op",
            "extra": "24385216 times\n4 procs"
          },
          {
            "name": "BenchmarkComputeDiff_NilSpec - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "24385216 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Small/3images",
            "value": 6342,
            "unit": "ns/op\t    4771 B/op\t      26 allocs/op",
            "extra": "186651 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Small/3images - ns/op",
            "value": 6342,
            "unit": "ns/op",
            "extra": "186651 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Small/3images - B/op",
            "value": 4771,
            "unit": "B/op",
            "extra": "186651 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Small/3images - allocs/op",
            "value": 26,
            "unit": "allocs/op",
            "extra": "186651 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Medium/5images",
            "value": 14159,
            "unit": "ns/op\t    5476 B/op\t      52 allocs/op",
            "extra": "85754 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Medium/5images - ns/op",
            "value": 14159,
            "unit": "ns/op",
            "extra": "85754 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Medium/5images - B/op",
            "value": 5476,
            "unit": "B/op",
            "extra": "85754 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Medium/5images - allocs/op",
            "value": 52,
            "unit": "allocs/op",
            "extra": "85754 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Large/40images",
            "value": 157845,
            "unit": "ns/op\t   17705 B/op\t     483 allocs/op",
            "extra": "6889 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Large/40images - ns/op",
            "value": 157845,
            "unit": "ns/op",
            "extra": "6889 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Large/40images - B/op",
            "value": 17705,
            "unit": "B/op",
            "extra": "6889 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromManifest/Large/40images - allocs/op",
            "value": 483,
            "unit": "allocs/op",
            "extra": "6889 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromMultipleManifests/TwoManifests",
            "value": 21628,
            "unit": "ns/op\t   10265 B/op\t      79 allocs/op",
            "extra": "59044 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromMultipleManifests/TwoManifests - ns/op",
            "value": 21628,
            "unit": "ns/op",
            "extra": "59044 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromMultipleManifests/TwoManifests - B/op",
            "value": 10265,
            "unit": "B/op",
            "extra": "59044 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromMultipleManifests/TwoManifests - allocs/op",
            "value": 79,
            "unit": "allocs/op",
            "extra": "59044 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromMultipleManifests/TenManifests",
            "value": 61174,
            "unit": "ns/op\t   47151 B/op\t     252 allocs/op",
            "extra": "19658 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromMultipleManifests/TenManifests - ns/op",
            "value": 61174,
            "unit": "ns/op",
            "extra": "19658 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromMultipleManifests/TenManifests - B/op",
            "value": 47151,
            "unit": "B/op",
            "extra": "19658 times\n4 procs"
          },
          {
            "name": "BenchmarkExtractImagesFromMultipleManifests/TenManifests - allocs/op",
            "value": 252,
            "unit": "allocs/op",
            "extra": "19658 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Simple",
            "value": 136.9,
            "unit": "ns/op\t      72 B/op\t       3 allocs/op",
            "extra": "8681664 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Simple - ns/op",
            "value": 136.9,
            "unit": "ns/op",
            "extra": "8681664 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Simple - B/op",
            "value": 72,
            "unit": "B/op",
            "extra": "8681664 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Simple - allocs/op",
            "value": 3,
            "unit": "allocs/op",
            "extra": "8681664 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/WithTag",
            "value": 110,
            "unit": "ns/op\t      48 B/op\t       2 allocs/op",
            "extra": "10880625 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/WithTag - ns/op",
            "value": 110,
            "unit": "ns/op",
            "extra": "10880625 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/WithTag - B/op",
            "value": 48,
            "unit": "B/op",
            "extra": "10880625 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/WithTag - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "10880625 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/DockerHubNamespaced",
            "value": 133.8,
            "unit": "ns/op\t      64 B/op\t       2 allocs/op",
            "extra": "8930390 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/DockerHubNamespaced - ns/op",
            "value": 133.8,
            "unit": "ns/op",
            "extra": "8930390 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/DockerHubNamespaced - B/op",
            "value": 64,
            "unit": "B/op",
            "extra": "8930390 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/DockerHubNamespaced - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "8930390 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/GHCR",
            "value": 109.5,
            "unit": "ns/op\t      48 B/op\t       1 allocs/op",
            "extra": "11085498 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/GHCR - ns/op",
            "value": 109.5,
            "unit": "ns/op",
            "extra": "11085498 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/GHCR - B/op",
            "value": 48,
            "unit": "B/op",
            "extra": "11085498 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/GHCR - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "11085498 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/RegistryK8s",
            "value": 109.4,
            "unit": "ns/op\t      48 B/op\t       1 allocs/op",
            "extra": "10902886 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/RegistryK8s - ns/op",
            "value": 109.4,
            "unit": "ns/op",
            "extra": "10902886 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/RegistryK8s - B/op",
            "value": 48,
            "unit": "B/op",
            "extra": "10902886 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/RegistryK8s - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "10902886 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Localhost",
            "value": 91.14,
            "unit": "ns/op\t      32 B/op\t       1 allocs/op",
            "extra": "13250581 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Localhost - ns/op",
            "value": 91.14,
            "unit": "ns/op",
            "extra": "13250581 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Localhost - B/op",
            "value": 32,
            "unit": "B/op",
            "extra": "13250581 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Localhost - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "13250581 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Digest",
            "value": 124.5,
            "unit": "ns/op\t     112 B/op\t       2 allocs/op",
            "extra": "9581646 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Digest - ns/op",
            "value": 124.5,
            "unit": "ns/op",
            "extra": "9581646 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Digest - B/op",
            "value": 112,
            "unit": "B/op",
            "extra": "9581646 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeImageRef/Digest - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "9581646 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_LocalhostWithPort",
            "value": 93.61,
            "unit": "ns/op\t     112 B/op\t       1 allocs/op",
            "extra": "12505826 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_LocalhostWithPort - ns/op",
            "value": 93.61,
            "unit": "ns/op",
            "extra": "12505826 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_LocalhostWithPort - B/op",
            "value": 112,
            "unit": "B/op",
            "extra": "12505826 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_LocalhostWithPort - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "12505826 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_ExternalRegistry",
            "value": 86.87,
            "unit": "ns/op\t     112 B/op\t       1 allocs/op",
            "extra": "14464323 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_ExternalRegistry - ns/op",
            "value": 86.87,
            "unit": "ns/op",
            "extra": "14464323 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_ExternalRegistry - B/op",
            "value": 112,
            "unit": "B/op",
            "extra": "14464323 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_ExternalRegistry - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "14464323 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_Empty",
            "value": 3.177,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "377671767 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_Empty - ns/op",
            "value": 3.177,
            "unit": "ns/op",
            "extra": "377671767 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_Empty - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "377671767 times\n4 procs"
          },
          {
            "name": "BenchmarkParseOCIURL_Empty - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "377671767 times\n4 procs"
          },
          {
            "name": "BenchmarkParseHostPort_WithPort",
            "value": 18.6,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "65278251 times\n4 procs"
          },
          {
            "name": "BenchmarkParseHostPort_WithPort - ns/op",
            "value": 18.6,
            "unit": "ns/op",
            "extra": "65278251 times\n4 procs"
          },
          {
            "name": "BenchmarkParseHostPort_WithPort - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "65278251 times\n4 procs"
          },
          {
            "name": "BenchmarkParseHostPort_WithPort - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "65278251 times\n4 procs"
          },
          {
            "name": "BenchmarkParseHostPort_ExternalNoPort",
            "value": 7.667,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "157093580 times\n4 procs"
          },
          {
            "name": "BenchmarkParseHostPort_ExternalNoPort - ns/op",
            "value": 7.667,
            "unit": "ns/op",
            "extra": "157093580 times\n4 procs"
          },
          {
            "name": "BenchmarkParseHostPort_ExternalNoPort - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "157093580 times\n4 procs"
          },
          {
            "name": "BenchmarkParseHostPort_ExternalNoPort - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "157093580 times\n4 procs"
          },
          {
            "name": "BenchmarkParseRegistryFlag_Simple",
            "value": 108.4,
            "unit": "ns/op\t     112 B/op\t       1 allocs/op",
            "extra": "10806912 times\n4 procs"
          },
          {
            "name": "BenchmarkParseRegistryFlag_Simple - ns/op",
            "value": 108.4,
            "unit": "ns/op",
            "extra": "10806912 times\n4 procs"
          },
          {
            "name": "BenchmarkParseRegistryFlag_Simple - B/op",
            "value": 112,
            "unit": "B/op",
            "extra": "10806912 times\n4 procs"
          },
          {
            "name": "BenchmarkParseRegistryFlag_Simple - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "10806912 times\n4 procs"
          },
          {
            "name": "BenchmarkParseRegistryFlag_WithCredentials",
            "value": 111.4,
            "unit": "ns/op\t     112 B/op\t       1 allocs/op",
            "extra": "10844286 times\n4 procs"
          },
          {
            "name": "BenchmarkParseRegistryFlag_WithCredentials - ns/op",
            "value": 111.4,
            "unit": "ns/op",
            "extra": "10844286 times\n4 procs"
          },
          {
            "name": "BenchmarkParseRegistryFlag_WithCredentials - B/op",
            "value": 112,
            "unit": "B/op",
            "extra": "10844286 times\n4 procs"
          },
          {
            "name": "BenchmarkParseRegistryFlag_WithCredentials - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "10844286 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatRegistryURL_WithPort",
            "value": 84.39,
            "unit": "ns/op\t      80 B/op\t       2 allocs/op",
            "extra": "14090137 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatRegistryURL_WithPort - ns/op",
            "value": 84.39,
            "unit": "ns/op",
            "extra": "14090137 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatRegistryURL_WithPort - B/op",
            "value": 80,
            "unit": "B/op",
            "extra": "14090137 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatRegistryURL_WithPort - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "14090137 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatRegistryURL_WithoutPort",
            "value": 66.14,
            "unit": "ns/op\t      48 B/op\t       1 allocs/op",
            "extra": "21360193 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatRegistryURL_WithoutPort - ns/op",
            "value": 66.14,
            "unit": "ns/op",
            "extra": "21360193 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatRegistryURL_WithoutPort - B/op",
            "value": 48,
            "unit": "B/op",
            "extra": "21360193 times\n4 procs"
          },
          {
            "name": "BenchmarkFormatRegistryURL_WithoutPort - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "21360193 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromViper_Set",
            "value": 210.6,
            "unit": "ns/op\t     144 B/op\t       3 allocs/op",
            "extra": "5669060 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromViper_Set - ns/op",
            "value": 210.6,
            "unit": "ns/op",
            "extra": "5669060 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromViper_Set - B/op",
            "value": 144,
            "unit": "B/op",
            "extra": "5669060 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromViper_Set - allocs/op",
            "value": 3,
            "unit": "allocs/op",
            "extra": "5669060 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromViper_Empty",
            "value": 128.9,
            "unit": "ns/op\t      32 B/op\t       2 allocs/op",
            "extra": "9302247 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromViper_Empty - ns/op",
            "value": 128.9,
            "unit": "ns/op",
            "extra": "9302247 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromViper_Empty - B/op",
            "value": 32,
            "unit": "B/op",
            "extra": "9302247 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromViper_Empty - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "9302247 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromConfig_LocalRegistry",
            "value": 268.9,
            "unit": "ns/op\t     112 B/op\t       1 allocs/op",
            "extra": "4467697 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromConfig_LocalRegistry - ns/op",
            "value": 268.9,
            "unit": "ns/op",
            "extra": "4467697 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromConfig_LocalRegistry - B/op",
            "value": 112,
            "unit": "B/op",
            "extra": "4467697 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromConfig_LocalRegistry - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "4467697 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromConfig_ExternalRegistry",
            "value": 273.9,
            "unit": "ns/op\t     112 B/op\t       1 allocs/op",
            "extra": "4404445 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromConfig_ExternalRegistry - ns/op",
            "value": 273.9,
            "unit": "ns/op",
            "extra": "4404445 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromConfig_ExternalRegistry - B/op",
            "value": 112,
            "unit": "B/op",
            "extra": "4404445 times\n4 procs"
          },
          {
            "name": "BenchmarkDetectRegistryFromConfig_ExternalRegistry - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "4404445 times\n4 procs"
          }
        ]
      }
    ]
  }
}