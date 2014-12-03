
FROM centos:centos6
MAINTAINER Traun Leyden <tleyden@couchbase.com>

ENV CB_VERSION		3.0.1
ENV CB_RELEASE_URL	http://packages.couchbase.com/releases
ENV CB_PACKAGE		couchbase-server-community-3.0.1-centos6.x86_64.rpm

# Install couchbase
RUN yum install -y wget pkgconfig openssl tar hostname
RUN rpm --install $CB_RELEASE_URL/$CB_VERSION/$CB_PACKAGE

# Put start script
ADD scripts/couchbase-start /usr/local/bin/

# See http://docs.couchbase.com/couchbase-manual-2.5/cb-install/#xdcr
EXPOSE 8091 8092 11210 11211

USER root
CMD couchbase-start 
