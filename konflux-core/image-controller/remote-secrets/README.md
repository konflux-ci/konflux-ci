Currently, image-controller has a dependency on the remote-secrets CRD,
which is deprecated. We only create it for letting the image-controller function
properly. There is no use of that CRD, and we don't deploy the remote-secrets operator.
