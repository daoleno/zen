package expo.modules.zenremotedesktop.moonlight;

import org.w3c.dom.Document;
import org.w3c.dom.Element;
import org.w3c.dom.NodeList;
import org.xml.sax.InputSource;
import org.xml.sax.SAXException;

import java.io.IOException;
import java.io.Reader;
import java.io.StringReader;

import javax.xml.parsers.DocumentBuilder;
import javax.xml.parsers.DocumentBuilderFactory;
import javax.xml.parsers.ParserConfigurationException;

/**
 * Host XML helper ported from upstream NvHTTP.getXmlString/verifyResponseStatus
 * (moonlight-android@98c12beb, app/src/main/java/com/limelight/nvstream/http/NvHTTP.java).
 *
 * The upstream implementation uses Android's XmlPullParser; this port keeps the
 * same semantics (status_code check on <root>, first matching tag text, required
 * vs optional) with the DOM API so the layer is also exercised by host JVM tests.
 */
public final class MoonlightXml {
    private MoonlightXml() {}

    public static String getXmlString(String xml, String tagName, boolean throwIfMissing)
            throws MoonlightParseException, HostHttpResponseException {
        return getXmlString(new StringReader(xml), tagName, throwIfMissing);
    }

    public static String getXmlString(Reader reader, String tagName, boolean throwIfMissing)
            throws MoonlightParseException, HostHttpResponseException {
        Document document;
        try {
            DocumentBuilderFactory factory = DocumentBuilderFactory.newInstance();
            factory.setNamespaceAware(false);
            factory.setExpandEntityReferences(false);
            factory.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
            factory.setFeature("http://xml.org/sax/features/external-general-entities", false);
            factory.setFeature("http://xml.org/sax/features/external-parameter-entities", false);
            DocumentBuilder builder = factory.newDocumentBuilder();
            document = builder.parse(new InputSource(reader));
        } catch (ParserConfigurationException | SAXException | IOException e) {
            throw new MoonlightParseException(e);
        }

        Element root = document.getDocumentElement();
        if (root == null) {
            throw new MoonlightParseException("Host response has no root element");
        }
        verifyResponseStatus(root);

        NodeList nodes = document.getElementsByTagName(tagName);
        if (nodes.getLength() == 0) {
            if (throwIfMissing) {
                throw new MoonlightParseException("Missing mandatory field in host response: " + tagName);
            }
            return null;
        }
        return nodes.item(0).getTextContent();
    }

    static void verifyResponseStatus(Element root) throws HostHttpResponseException {
        String statusCode = root.getAttribute("status_code");
        if (statusCode.isEmpty()) {
            // Upstream treats <root> status_code as mandatory in practice; keep
            // parsing tolerant for hosts that omit it while still validating == 200.
            return;
        }
        // Long.parseLong matches upstream: GFE can send 0xFFFFFFFF which would
        // overflow Integer.parseInt; casting the long yields the -1 error code.
        int code = (int) Long.parseLong(statusCode);
        if (code != 200) {
            String statusMsg = root.getAttribute("status_message");
            if (code == -1 && "Invalid".equals(statusMsg)) {
                code = 418;
                statusMsg = "Missing audio capture device. Reinstall GeForce Experience.";
            }
            throw new HostHttpResponseException(code, statusMsg);
        }
    }
}
