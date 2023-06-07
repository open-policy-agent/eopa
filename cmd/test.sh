#!/bin/bash 

# exercising keygen.sh API endpoint: to get rate limit errors
# Note: not a good idea to run in CI or continuosly (uses a significant number of daily keygen API requests)
# run this helper script manually

set -e

x=1
while [ $x -le 60 ]
do
  ../bin/eopa eval -b . data.test.allow -l debug -f pretty
  echo $?
  ../bin/eopa license
  echo $?
  x=$(( $x + 1 ))
done
echo "Done"
