#!/bin/bash

FILE=/Users/berah/work/finconecta/forward-iac-landing-zone/0-landing/scripts/tagging.txt

for item in $(cat $FILE); do
  org=$(echo $item | cut -d':' -f1)
  ou=$(echo $item | cut -d':' -f2)
  app=$(echo $item | cut -d':' -f3)
  apptype=$(echo $item | cut -d':' -f4)
  acct=$(echo $item | cut -d':' -f5)
  role=$(echo $item | cut -d':' -f6)

  assume=""
  if [ "$role" != "" ] ; then
    assume="--assume-role-arn arn:aws:iam::$acct:role/$role"
  fi

  ./tronador-cli aws tag --organization $org --organization-unit $ou --application-name $app --application-type $apptype \
    --target resources $assume \
    --types eventbus \
    --region eu-west-2
done