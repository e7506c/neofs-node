package blobstor

// DeleteSmallPrm groups the parameters of DeleteSmall operation.
type DeleteSmallPrm struct {
	address
	rwBlobovniczaID
}

// DeleteSmallRes groups the resulting values of DeleteSmall operation.
type DeleteSmallRes struct{}

// DeleteSmall removes an object from blobovnicza of BLOB storage.
//
// If blobovnicza ID is not set or set to nil, BlobStor tries to
// find and remove object from any blobovnicza.
//
// Returns any error encountered that did not allow
// to completely remove the object.
//
// Returns an error of type apistatus.ObjectNotFound if there is no object to delete.
func (b *BlobStor) DeleteSmall(prm DeleteSmallPrm) (DeleteSmallRes, error) {
	return b.blobovniczas.delete(prm)
}
