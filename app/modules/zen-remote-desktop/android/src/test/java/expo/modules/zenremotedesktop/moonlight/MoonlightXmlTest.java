package expo.modules.zenremotedesktop.moonlight;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertThrows;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public class MoonlightXmlTest {

    @Test
    public void extractsTagsAndValidatesStatus() throws Exception {
        String xml = "<?xml version=\"1.0\"?><root status_code=\"200\">"
                + "<hostname>zen-host</hostname><appversion>7.1.431.0</appversion>"
                + "<PairStatus>1</PairStatus></root>";
        assertEquals("zen-host", MoonlightXml.getXmlString(xml, "hostname", true));
        assertEquals("1", MoonlightXml.getXmlString(xml, "PairStatus", true));
        assertNull(MoonlightXml.getXmlString(xml, "GfeVersion", false));
    }

    @Test
    public void missingMandatoryFieldThrows() {
        String xml = "<root status_code=\"200\"><paired>1</paired></root>";
        assertThrows(MoonlightParseException.class,
                () -> MoonlightXml.getXmlString(xml, "challengeresponse", true));
    }

    @Test
    public void nonOkStatusThrowsHostException() {
        String xml = "<root status_code=\"503\" status_message=\"busy\"></root>";
        HostHttpResponseException error = assertThrows(HostHttpResponseException.class,
                () -> MoonlightXml.getXmlString(xml, "paired", true));
        assertEquals(503, error.getErrorCode());
    }

    @Test
    public void unsignedStatusOverflowIsCarriedLikeUpstream() {
        String xml = "<root status_code=\"4294967295\" status_message=\"Invalid\"></root>";
        HostHttpResponseException error = assertThrows(HostHttpResponseException.class,
                () -> MoonlightXml.getXmlString(xml, "paired", true));
        assertEquals(418, error.getErrorCode());
    }

    @Test
    public void doctypeIsRejected() {
        String xml = "<!DOCTYPE root [<!ENTITY x SYSTEM \"file:///etc/passwd\">]>"
                + "<root status_code=\"200\"><paired>&x;</paired></root>";
        assertThrows(MoonlightParseException.class, () -> MoonlightXml.getXmlString(xml, "paired", true));
    }

    @Test
    public void garbageXmlThrowsParseException() {
        assertTrue(assertThrows(MoonlightParseException.class,
                () -> MoonlightXml.getXmlString("not xml at all", "paired", true)) != null);
    }
}
