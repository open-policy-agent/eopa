package transform_test

import rego.v1

_kafka_msg(topic, payload) := {
	"topic": topic,
	"key": base64.encode(json.marshal(payload.id)),
	"value": base64.encode(json.marshal(payload)),
}

# some often-used messages
ada := _kafka_msg("users", {"id": "ada", "name": "Ada Lovelace"})

margherita := _kafka_msg("users", {"id": "maga", "name": "Margherita Hack"})

scientists := _kafka_msg("groups", {"id": "sci", "name": "scientists"})

explorers := _kafka_msg("groups", {"id": "exp", "name": "explorers"})

test_one_topic_one_message if {
	new := transform with input.previous as {}
		with input.incoming as [ada]
	new == {"users": {"ada": {"name": "Ada Lovelace"}}}
}

test_one_topic_two_messages if {
	new := transform with input.previous as {}
		with input.incoming as [ada, margherita]
	new == {"users": {"ada": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}}
}

test_one_topic_with_updates if {
	# alice gets an update, ruth's record must be kept, maga is deleted
	new := transform with input.previous.users as {"ada": {"name": "Ada Lovelace", "old": "record"}, "ruth": {"name": "Ruth Harkness"}} # regal ignore:line-length
		with input.incoming as [ada, _kafka_msg("users", {"id": "maga", "type": "delete"})]
	new == {"users": {"ada": {"name": "Ada Lovelace"}, "ruth": {"name": "Ruth Harkness"}}}
}

test_one_topic_with_updates_numeric_keys if {
	# alice gets an update, ruth's record must be kept, maga is deleted
	new := transform with input.previous.users as {1: {"name": "Ada Lovelace"}, 2: {"name": "Ruth Harkness"}}
		with input.incoming as [_kafka_msg("users", {"id": 2, "name": "Margherita Hack"}), _kafka_msg("users", {"id": 3, "type": "delete"})] # regal ignore:line-length
	new == {"users": {1: {"name": "Ada Lovelace"}, 2: {"name": "Margherita Hack"}}}
}

test_two_topics_two_messsages if {
	new := transform with input.previous as {}
		with input.incoming as [ada, margherita, scientists]
	new == {"groups": {"sci": {"name": "scientists"}}, "users": {"ada": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}} # regal ignore:line-length
}

test_two_topics_updates_to_only_one_topic if {
	new := transform with input.previous as {"groups": {"sci": {"name": "scientists-with-typo"}}, "users": {"ada": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}} # regal ignore:line-length
		with input.incoming as [explorers, scientists]
	new == {"groups": {"exp": {"name": "explorers"}, "sci": {"name": "scientists"}}, "users": {"ada": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}} # regal ignore:line-length
}

test_two_topics_updates_to_each_topic if {
	new := transform with input.previous as {"groups": {"sci": {"name": "scientists-with-typo"}}, "users": {"ada": {"name": "Ada Lovelace", "old": "record"}, "maga": {"name": "Margherita Hack"}}} # regal ignore:line-length
		with input.incoming as [ada, scientists]

	new == {"groups": {"sci": {"name": "scientists"}}, "users": {"ada": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}} # regal ignore:line-length
}

test_two_topics_updates_with_key_clash if {
	# ada has a "sci" key now just for testing the disambiguation per topic
	new := transform with input.previous as {"groups": {"sci": {"name": "scientists-with-typo"}}, "users": {"sci": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}} # regal ignore:line-length
		with input.incoming as [scientists]
	new == {"groups": {"sci": {"name": "scientists"}}, "users": {"sci": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}} # regal ignore:line-length
}

test_two_topics_delete_with_key_clash if {
	# ada has a "sci" key now just for testing the disambiguation per topic
	new := transform with input.previous as {"groups": {"sci": {"name": "scientists-with-typo"}}, "users": {"sci": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}} # regal ignore:line-length
		with input.incoming as [_kafka_msg("groups", {"id": "sci", "type": "delete"})]
	new == {"users": {"sci": {"name": "Ada Lovelace"}, "maga": {"name": "Margherita Hack"}}}
}

test_unspecific_topic_works if {
	new := transform with input.previous as {"groups": {"sci": {"name": "scientists"}}, "users": {"ada": {"name": "Ada Lovelace"}}} # regal ignore:line-length
		with input.incoming as [_kafka_msg("roles", {"id": "admin", "values": {"what": "ever"}})]
	new == {
		"roles": {"admin": {"id": "admin", "values": {"what": "ever"}}},
		"groups": {"sci": {"name": "scientists"}},
		"users": {"ada": {"name": "Ada Lovelace"}},
	}
}

test_unspecific_topic_updates if {
	new := transform with input.previous as {"roles": {"admin": {"id": "admin", "values": {"what": "ever"}}}}
		with input.incoming as [_kafka_msg("roles", {"id": "admin", "permissions": "all"})]
	new == {"roles": {"admin": {"id": "admin", "permissions": "all"}}}
}

test_kind_from_topic_one_message if {
	new := transform with input.previous as {}
		with input.incoming as [_kafka_msg("group-topic", {"id": "sci", "name": "scientists"})]
	new == {"groups": {"sci": {"name": "scientists"}}}
}

test_kind_from_topic_with_updates if {
	# alice gets an update, ruth's record must be kept, maga is deleted
	new := transform with input.previous.groups as {"sci": {"name": "scientists", "old": "record"}}
		with input.incoming as [_kafka_msg("group-topic", {"id": "sci", "name": "scientists"})]
	new == {"groups": {"sci": {"name": "scientists"}}}
}
