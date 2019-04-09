#!/usr/bin/env bash

source ../../local.config

#current workdir
cwd=${PWD}

#Activate single sign on
echo "Step X/X: Enable Jira SSO"
cd ${cwd}
vagrant ssh atlcon -c "cd /vagrant/ansible/ && export ANSIBLE_VAULT_PASSWORD_FILE=/vagrant/ansible/.vault_pass.txt && ansible-playbook -v -i inventories/dev playbooks/jira-enable-sso.yml"

echo "Step X/X: Enable Confluence SSO"
cd ${cwd}
vagrant ssh atlcon -c "cd /vagrant/ansible/ && export ANSIBLE_VAULT_PASSWORD_FILE=/vagrant/ansible/.vault_pass.txt && ansible-playbook -v -i inventories/dev playbooks/confluence-enable-sso.yml"

echo "Step X/X: Mirror repositories to ${TARGET_REPO_BASE}"
cd ${cwd}/scripts
./mirror-repositories-to-gitserver.sh

echo "Step X/X: Connect to openshift VM and prepare OpenShift cluster"
vagrant ssh openshift -c "/ods/ods-core/infrastructure-setup/scripts/prepare-openshift.sh"

sleep 5s

echo "Step X/X: Add OpenShift certificate to atlassian VM"
vagrant ssh atlassian -c "/ods/ods-core/infrastructure-setup/scripts/import-certificate-to-atlassian-jvm.sh"

echo "Step X/X: Prepare OpenDevStack Template project"

echo "Step X/X: Prepare Nexus"

echo "Step X/X: Prepare Sonarqube"

echo "Step X/X: Prepare Jenkins"

