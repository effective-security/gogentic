#!/bin/bash
source .project/yaml.sh
create_variables .project/config.yml

ORG_NAME=$project_org

if [ -z "${ORG_NAME##*/*}" ] ;then
    export REL_PATH_TO_GOPATH=../../../..
else
    #echo "'$ORG_NAME' does not contain: '/'."
    export REL_PATH_TO_GOPATH=../../..
fi

echo $REL_PATH_TO_GOPATH