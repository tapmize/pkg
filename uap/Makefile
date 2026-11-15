PYTHON ?= python3
CURL ?= curl
UA_DATE ?= $(shell date -u +%F)
UAP_CORE_REF ?= master
UAP_REGEX_URL ?= https://raw.githubusercontent.com/ua-parser/uap-core/$(UAP_CORE_REF)/regexes.yaml

.PHONY: update update-catalog update-uap

update: update-catalog update-uap

update-catalog:
	$(PYTHON) update_catalog.py --date "$(UA_DATE)"

update-uap:
	$(CURL) -fsSL "$(UAP_REGEX_URL)" -o uap_regexps.yaml.tmp
	test -s uap_regexps.yaml.tmp
	grep -q '^user_agent_parsers:' uap_regexps.yaml.tmp
	mv uap_regexps.yaml.tmp uap_regexps.yaml
