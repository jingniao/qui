package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"
)

// applied pins the SHA-256 of every migration that may already be applied in a
// deployed database. db.Open and backup validation compare these bytes against
// schema_migrations.checksum, so changing a single byte (including a line
// ending) makes existing panels refuse to start and old backups fail to
// restore. Add new migrations here; never change an existing entry.
var applied = map[string]string{
	"0001_bootstrap.sql":                                "c76cde72c30e69b3427f5c1f445dd5851ba1612bd998d5d08a5a423c5236d42b",
	"0002_auth.sql":                                     "d8759fd5eb5cfd836991dc655e44c4dfbcba9168d34ab12556c037734b809dc1",
	"0003_nodes.sql":                                    "8ad8d20053a42cc135efaeea79c2305994155e8593624a639dfe6de1834c0a1f",
	"0004_deployment.sql":                               "ba7844bdf512fe40916f44976280e061fe4f3f2177c5c373cac04f46328bc55c",
	"0005_users_groups.sql":                             "3259316dc6a42d93411d5872e443bfbf4ae2e71e1eff0624e6e0eedfe3f0e073",
	"0006_proxy.sql":                                    "9434ff94b37b172a5f02f3cf703db98c28d8f1275d025cbe6df699e8188ba05d",
	"0007_relay_bindings.sql":                           "4ef11f34fdf15995d7b0366479e8bbcaba7ef805bf91015c8873139304ff528d",
	"0008_wireguard_addresses.sql":                      "d6426f4dc7c0739c20145ef42467d099c6d405ee78110697a35610b250a5874f",
	"0009_policy.sql":                                   "2ccf79dc7fa4f7213726e42bac09fa71b31893d6a2166edbd4caa29f892ed845",
	"0010_groups_chains.sql":                            "9096cbbc841d09fc36b47bf9e8d286281799748519b27f52f414d257d7456155",
	"0011_outbound_probes.sql":                          "46c50ab9e4acb73d0e2109e2a15d2af74fe8e4b196a2cd86a1f1e785fd70fc97",
	"0012_deployment_batches.sql":                       "77b8785cbb516f5616df69902e6067d8f6291128e9e7d0c510e5ad0676ecedbe",
	"0013_fallback_targets.sql":                         "8ed497335bc79471ab4ca180d99a3f12c388914d39108c38f955884691185957",
	"0014_access_telemetry.sql":                         "c2914c3c4ed7b7400f345e07c216877902f62e924c4395ad31cb5589e0acbfde",
	"0015_account_traffic.sql":                          "1e23b1c86c039bfe4e4d7f41f42268d683bafd116ea02f6b98bc36b2fdb38079",
	"0016_traffic_cursor_identity.sql":                  "5618c9e1e3b0d68de8958f5a48ee182bd49c379d577b1cf41add24ff63d4c643",
	"0017_node_budgets.sql":                             "cd488589de5a45f1dcafba53bba0119906820c3fa94eb88eff3a79f4b7b8a8d6",
	"0018_backups.sql":                                  "9430f54ca14516c09b83a8b5e7f2ab833bf2bd90bd32c3bad8cf41ac0aac816d",
	"0019_backup_runs.sql":                              "4a8c6ab95f6b4d5c70b38fb280dbe27694ec0bb8e168b5223bec5c471086db4c",
	"0020_backup_file_ops.sql":                          "a4336e2707bd3214bf1ea9c97b734c2996052aaf4c8d09bc27629a02e1aab4e2",
	"0021_backup_origin.sql":                            "ad86229ebce1d58a1b3176ed5e7416a3f7da660fc1367e4e079e43f28d19baf1",
	"0022_node_certificate_renewals.sql":                "3dba7bc96fad021314c9c2b30f940d928ce00a134abb34d74b6e9a61ec0b07d0",
	"0023_final_usage.sql":                              "c84298b17b129262ebb92ec911554eabde37e4f7891e566a3eaee035a25336a7",
	"0024_runtime_final_usage.sql":                      "3e9248a52cbafe2cdae620128fb78ccae027133f2831d58991688c80b44a40b0",
	"0025_runtime_sources.sql":                          "0de2c3199356ba42f53c9d653b246412bd7a083081654ee2084d0ca26a3d48da",
	"0026_quota_conversion.sql":                         "18b60fa41b8980d090f00dd86a1a236cff3a9fa7a04fdcdd0ecbdc6a35726894",
	"0027_unissued_budget_returns.sql":                  "3bc1ba9a14cd24d864dce529010373e8731bf29a601e4cb2fd0bea1bd3d554f8",
	"0028_issued_budget_returns.sql":                    "7127b803c03a7638f59e18dcd1c0621a6869b5823802069fc2ddd4686dd13ecb",
	"0029_budget_transfers.sql":                         "d2150ecc1a69490e59e2233c237db92b818ff13476b418963a61fa026f8edac2",
	"0030_budget_transfer_cancellation.sql":             "d67c77fcac13159ce968c1dcce2a4969dd5238d7b77c8d8ca0f5d8bb5f8d528e",
	"0031_transfer_reclaims.sql":                        "1548e94c609dec23f7f63758d3e5b1c487694d3a84c04f641ec3f9e65cd30cda",
	"0032_transfer_target_returns.sql":                  "1c7b2cc3c0a48b2eb799069dbde94743afb1238c7c42aa031f748bfa892a0cdd",
	"0033_automatic_transfer_redirect.sql":              "eeb9f5ab39457a342fbfa96b0993f0eda3c118f23aa39fd5e2aae0054b8fc386",
	"0034_node_pause.sql":                               "feb328367d013c25770c63ec8a0d1f305ac11890358a94b4b3db2f90d106accf",
	"0035_core_updates.sql":                             "d977f559232334277361d959a1bb03d5d7766b0cdfdd3a56b612d4667022fb42",
	"0036_agent_updates.sql":                            "f176741d723494a046bd28649297fc9ca2b500e1480bf605132c4661ca81d698",
	"0037_agent_recovery.sql":                           "8604e29ee4d3c94f399b51eb5fd448af7b7e49831a7c49212e69789bb8f1434c",
	"0038_connection_allowances.sql":                    "664c4823b94e88c0133dffc3afedd94ebe8dfd70d7a1174b0b8775275e126945",
	"0039_connection_generations.sql":                   "478a11aaf4f66382c19f378d4fe377af98a2fb32a80bed2d9ba4c5861ce6a951",
	"0040_live_connections.sql":                         "900febe24319709d156ec8998ea935a6fccf14cba6074ab569947dfe4077d0df",
	"0041_node_health_checks.sql":                       "af27675ccb9144715508354014f3f0b450c44f332a78235dedf2a550f6baee8b",
	"0042_node_restart_requests.sql":                    "acd84941e5bda8d054ed7f8288acfe822f1a26d68df9ec8d84db6ab3134ffb6d",
	"0043_node_ca_trust_receipts.sql":                   "fcd44fb5384892e949544bca54291727a80ec294540648dbbd6adefb81f31a12",
	"0044_node_ca_transition.sql":                       "8495aa24e749c847ad4082761db0f99f23c7bc71429a0aa77f958dae674dca87",
	"0045_node_ca_retirement_receipts.sql":              "dda8ce704c7faea69ad7f5d58e2252d9054023c18ff492413c5070eb5d348c18",
	"0046_node_ca_global_retirement.sql":                "ec1e75084732bb7405d70ed3b27c4b934b1946b79cba4334b26d7426c75b40f0",
	"0047_wireguard_dual_stack.sql":                     "9ebbe7fdb16bef510b3fbf1ab6118f4afb1f7aed7c4dc7dbd2e00f631a6bb153",
	"0048_traffic_pending_batches.sql":                  "190d910de3dbbb27273c4a27b40d4469b61f97a1a252fb30fe3b114837758c04",
	"0049_node_ca_successor_rounds.sql":                 "067f8c907f3977162478aa55495479fd99bf9c521be0a75ecad9bf037160ccc3",
	"0050_node_ca_successor_commits.sql":                "b4669a656d04d6686db4603360ea54c979007cae1cae30d780ca781de4013875",
	"0051_node_ca_successor_retirement_assessments.sql": "eb707921159406292a9e09bdc8258d3bebd6003be6df42f198f99441ed5999c4",
	"0052_node_identity_recovery_decisions.sql":         "f2ce68bbf8312d2f294d535812a9ad798cbcea8958bd9072359367c5221675d3",
	"0053_recovery_cases.sql":                           "ac0727e4477f6489d9c3221d8d822690b42611a01d511833a55fcbde35715156",
	"0054_recovery_case_decisions.sql":                  "b2523a7d3cffec4851586cf590b789f4d25dbdc8404691b6555432044331d03c",
	"0055_recovery_evidence_results.sql":                "e1c1ecc41397186ba2eaac17bbd620ba05d0cd48a97b4ba3926c5e3c56d1305a",
	"0056_access_retention_job.sql":                     "c52170bd3d04a6972ca358ea855a5a8e4b40fb63ffdb7838077d759f1ede2d14",
	"0057_access_space_maintenance.sql":                 "e8f3f7785a06da19f5b6468ca77c1eacdaa6839dfc2d3d09723c7f0ed587df13",
	"0058_recovery_accounting_checks.sql":               "fc5724d159354c78ec8b9ee0bb079390af27fbfbf5bc1ff6cf82d91f65302bc8",
	"0059_traffic_accounting_completion.sql":            "4ce26f5312127f79c7eb9823114ea1df8b81d080496be646c65f8ab769a5e6b4",
	"0060_recovery_traffic_executions.sql":              "3cad265b4f0f6bd23bfb9c8b8d2fc5da36d96b8cd86981ccd3f8c83b3253e20e",
	"0061_recovered_budget_retirements.sql":             "746d8da82e0a6d836bf76f3280dc6170baeccf086a10d89324eade8d96a16699",
	"0062_access_receipt_archives.sql":                  "e6056f00dc21c7cdf11e18b0c61e317a3ba4f78055542ac8779137ef27e81592",
	"0063_traffic_receipt_archives.sql":                 "c73701316f677ace04314f4edda568893cb09339f9b4288a9e1432af54d8dc54",
	"0064_receipt_archive_job.sql":                      "1552577ceab91ebac7aa729de61428d9dfcd2185b2d62ec32a4f610e6cfdf570",
	"0065_receipt_archive_cursors.sql":                  "8c388ed7b376958e9f092b1e652ee7aae223c1a8004a3e27dd13e9a14fe430d3",
	"0066_node_accepted_messages.sql":                   "b142272fdbdde9013273f3473889792372be248e9396c42c9168c8740910dc2f",
	"0067_node_identity_replacements.sql":               "e0b755a3b0f9cbffd529b63c996b34c0d59aaeb24ee9fe671079f58ea56e56c7",
	"0068_node_enrollment_receipts.sql":                 "a60ae390d7e6e36cd0753868a02c163b79843ddce6757159f9e1a3482fc9bdc4",
	"0069_node_loss_state_reviews.sql":                  "434d9fe1147e867b23af385c5437b6284ba79b345e221896d8deef3fe34e8807",
	"0070_node_loss_dispositions.sql":                   "c805fa3dcc69a1e9526c232ca5ddef305b9d58c1e376ac25a348b262b895e605",
	"0071_owner_expired_certificate_renewals.sql":       "704050e768c45fa2ab897b12bd7570947d6ff6e6c456d334fe776e3f824ef022",
	"0072_owner_renewal_original_material.sql":          "bf189e71d44e2f56ce70d0e9b8bbf1e2694290ebdd6b32b92e5350ad8f97b82f",
	"0073_owner_renewal_previous_ca.sql":                "9f6624fcac2dce7f274ebe9702fe6a48d90318a883dee169a3ab16d6252c72af",
	"0074_recovery_revision_applications.sql":           "e08ca4646b1b1df95a9091727dc2a82b58a67851d7dc2656466b4fdf29f6ea0f",
	"0075_recovery_identity_completions.sql":            "75a31fc777911decdd950fb90457401d4cc4d327657d65b4b12d8b2baa125dd0",
	"0076_recovery_message_completions.sql":             "d8359d2287df46131ca70da1b664e81f7243885a1dbe10651f5e6221f20a43ea",
	"0077_recovery_update_completions.sql":              "6880ab3140b8f030732b454da0bde7c1a53aedca7fc86f41395862d678d877f2",
	"0078_recovery_agent_result_completions.sql":        "d2d7db7e17cbb3736f596fd3af9a5f1e0e0083cedc1fff0f20738fd31d6c2c3a",
	"0079_recovery_core_result_completions.sql":         "fab83412d83f20c0f457a863fc5ea69374d7552c38dc5517195bed1e076ebf27",
	"0080_recovery_final_settlements.sql":               "34251105cce6e15344bdaf3fe44a4ea43d3146d2ca2251bbf4e2ed7c2f9936a8",
	"0081_recovery_connection_completions.sql":          "fb7d5c3922e62eed0be1c8cc06e295a9918901f1dd0862febb300f1f3e0b9be0",
	"0082_recovery_connection_adoptions.sql":            "ebdd9ecbcad9c9580d93af889c72e7417ce3f7804157fb72b39f8b90817e48dc",
	"0083_recovery_budget_adoptions.sql":                "8354c4e86ddd08a7d4b6f027e8ed9b69e94920eb8ac387ec07b6645e0efd76a4",
	"0084_controller_handoff_completions.sql":           "731e2e7a11b2939fa8650ae4fb6cabc09151f92dfbfbf9646a85179eb918d438",
	"0085_recovery_owner_credentials.sql":               "544dcc16718afb60b4784e9b5bdd4bb383341ff12da0248f04f46789bacab51e",
	"0086_node_ca_successor_recovered_round.sql":        "ba014fee2ee27a75d5a4a2b19905adc2c59d9e8baafd1d67bc66d073d8aba200",
	"0087_recovery_budget_unknown_dispositions.sql":     "988ee7718aa276e3b62cc7ab7d4ea7c25ae4df512423db85ee77d38e76ff62f1",
	"0088_node_identity_isolations.sql":                 "d52203e91698a22db4e9c1833c4f51ef6f0c4f41cf62d747c7006fb9085f62f9",
	"0089_controller_isolation_evidence.sql":            "046cb4cf2fc464de3800874b81b75d0d175236024bd8fe78105d0a3c862de9d8",
	"0090_connection_observations.sql":                  "c6af736a8a06036d82f97f01b1c2a61369ff4f42c913df506f77fadebefce866",
	"0091_egress_replay_guards.sql":                     "d6dbf94c38f38a8068a0008b2a29073d5a41c474143ec59b5ccce263c6d028b1",
	"0092_node_ca_recovered_root.sql":                   "7f3a1e3755f773d14adcfbc33e7ccddded2d955665e8a9b419b7f070c8023f7e",
	"0093_node_ca_recovered_root_binding.sql":           "3a05fcaca5e0d0c31328c3d4817814bf8f437be822ffbcedc9e7a360b3941269",
	"0094_node_ca_recovered_root_base.sql":              "68011ed1d93596cf6c3e7b9906b3c1566acb6f6398060600db6dd2d5e74af7d8",
	"0095_node_ca_recovered_root_original_chain.sql":    "cd43e9fe79160a6b025e5ded222646b34607d462fb2698b437d307c13ae40a25",
	"0096_node_ca_operation_requests.sql":               "4454638e9ad58c3f792f9c2493d917aae16c75b0eb3130f227d453ebfeaf1b66",
	"0097_node_ca_operation_claim.sql":                  "f4cf1b5c357ab685e95e31ba86f914c0ef4d272cd5b70dd8c80eeb2f8e21d368",
	"0098_controller_isolation_approval.sql":            "b24d594f9a7e3bd330be99272dd3c7fd1b89aeaa952f34dfd8dcd178290a6646",
	"0099_drop_receipt_archives.sql":                    "f313c63faf76f9943afee30daa02c49a7790be3c10494dfd81390c83db7a302e",
	"0100_drop_recovery_cases.sql":                      "1e68fede6829dc8cb7bce5b1489e2fa7dd5bf9833d53c1b72885352ef7afde17",
	"0101_single_node_ca.sql":                           "58b4993b9a1a9ec21724110c6ff16dcd402e29b507d584168b29730ce7eb91c2",
	"0102_drop_node_budgets_and_connection_ledger.sql":  "5c73021ed28ae27554f36b077b333f1a0382771e67c471e7cbb29a2f55e9a6e4",
	"0103_drop_node_live_connections.sql":               "7e1361a78cf4b3b18ce17852337109c8463de0a42d1daa8a1e5bfc9f009f06ea",
	"0104_egress_weighted_to_round_robin.sql":           "e74f8003c3077cbdcae46a47f6924d9b15c31b76e7573a9bebe093bd1d745eab",
	"0105_drop_fork_observation.sql":                    "9290d1d0cae130ea3c7060270b832596ca672b849ba87b133cea7e69a925a436",
	"0106_nullable_access_bytes.sql":                    "8651342ef99a26227ce6bd76a40c8368ac09e0566ffc5bbe6f7b164df6ad1eb7",
	"0107_core_update_direct_url.sql":                   "35465d33968630b82c9a51ac32d195db864ca6002eb378b938899ae15c4cdc33",
	"0108_node_outbound_network.sql":                    "ce48a86f17740c59cdd422ebca61cabed20c934e0c4240d6f35d22177826168a",
	"0109_node_deletion.sql":                            "607ce78c0f25e79727a2fbd5b0a527743df84e8548e99ebed91e84ff4e8eb2e4",
	"0110_credential_tombstones.sql":                    "9651c14984afa849b6b822dd4f253ea340ec008803cb5153e3fe49f95d976339",
	"0111_inbound_enabled_intent.sql":                   "9ab9ca8a3dd2cdaa80060bb68435f18c53426b7c4abf1d26dabc86ecf4783730",
	"0112_canvas_schemes.sql":                           "e31c9f3079971ed90b8be2348eb73018c2b1070d282bbdacc517237f7ec784fa",
	"0113_user_level_access.sql":                        "e6e210fc3b9ed37a8dd205d9bf38f154f813dec6d18932fcd03fc7389f055c5d",
	"0114_end_user_accounts.sql":                        "2e6451f4e5ad55d307400ac4e1c499ca5e07fc08c40b6ec389e1cbc75d2f74c8",
	"0115_device_limit.sql":                             "c18cf852cb60561fe49bf0d537217d5cfde1ffeaab880639e08a4086438ac7b0",
	"0116_subscription_sources.sql":                     "585677d162a733b66dee0fbb3ac4b6aceebc32e7758148f39dc9b47bd51e027f",
	"0117_node_meta.sql":                                "e52ba9d90d67a6bcc7e3b967f360ad6406e2891e30b593d8eb4907cfbddbf9f8",
	"0118_traffic_reset.sql":                            "28f6418c60276d50c71738a7ce7a28e4a20ea1885f3a0f70196c69e0604c2d96",
	"0119_tcp_probes.sql":                               "ac045ecd3d31d967e6c83afa1102025912f8331480d13ac51fda86ed0a38c6b7",
	"0120_latency_samples.sql":                          "f3a9232b8851ed3c91a7df4a7e50efd8d7b5c6178d0a8e604ee956bd3c0f043d",
}

func TestAppliedMigrationsAreImmutable(t *testing.T) {
	names, err := fs.Glob(Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		data, err := fs.ReadFile(Files, name)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		want, ok := applied[name]
		if !ok {
			t.Errorf("%s is not pinned in applied; add its checksum", name)
			continue
		}
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Errorf("%s changed: checksum %s, pinned %s", name, got, want)
		}
	}
	for name := range applied {
		if _, err := fs.Stat(Files, name); err != nil {
			t.Errorf("pinned migration %s is missing", name)
		}
	}
}
