#!/bin/bash
source .project/yaml.sh
create_variables .project/config.yml
eval $(printf "echo $%s" "$1")
