

microci
=======

Minimalistic CI framework for running tasks triggered by gitea webhooks

Installation
------------

go get github.com/yzzyx/microci

Basics
------

When microci is triggered, it will perform the following tasks:

* Clone/update the repository that triggered the change
* Cancel currently running tasks for the same pull-request if one exists
* Execute the appropriate script, based on repository and type of trigger
* Check the return value of the script, and update the status in gitea accordingly

Setup
-----

Start by create a new folder to keep all configuration files in.

For each repository microci will be receiving information from, create a subfolder with the
same name. (if the repository is located at "yzzyx/microci", create that folder structure).

When a task is triggered, one of the following scripts in that folder will be executed, depending on 
the event type:

* push.sh
* pull-request.sh

actions for pull-requests:
* `label_updated`
* `opened`
* `syncronized`
* `closed`


If actions should only be applied to specific branches, create a subfolder for that branch name.

Example structure:

```
 |- yzzyx
   |- microci
     |- master
       |- push.sh         - will be triggered when changes are pushed to branch master
       |- pull-request.sh - will be triggered when a pull-request is opened against master
     |- push.sh            - will be triggered for all branches except master
```

Variables
---------

The following variables are available for usage in scripts

| Variable | Description |
|----------|-------------|

(FIXME - add complete list)
