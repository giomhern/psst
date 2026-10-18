import Foundation
import Contacts

private struct Request: Decodable {
    let v: Int
    let handles: [String]
}

private struct Response: Encodable {
    let v = 1
    let status: String
    var names: [String: String] = [:]
    var failure: String? = nil
    var outcomes: [String: Int] = [:]
}

@main
private struct ContactsHelper {
    static func status() -> String {
        switch CNContactStore.authorizationStatus(for: .contacts) {
        case .notDetermined: return "permission_required"
        case .restricted: return "restricted"
        case .denied: return "denied"
        case .authorized: return "authorized"
        case .limited: return "limited"
        @unknown default: return "unavailable"
        }
    }

    static func emit(_ response: Response) {
        // Never print NSError descriptions or records outside this bounded protocol.
        guard let data = try? JSONEncoder().encode(response) else { return }
        FileHandle.standardOutput.write(data)
    }

    static func request() throws -> Request {
        var input = Data()
        while let chunk = try FileHandle.standardInput.read(upToCount: 65536), !chunk.isEmpty {
            input.append(chunk)
            if input.count > 1_048_576 { throw CocoaError(.fileReadTooLarge) }
        }
        let request = try JSONDecoder().decode(Request.self, from: input)
        guard request.v == 1, request.handles.count <= 500,
              request.handles.allSatisfy({ !$0.isEmpty && $0.utf8.count <= 320 }) else {
            throw CocoaError(.coderReadCorrupt)
        }
        return request
    }

    static func main() async {
        let args = Array(CommandLine.arguments.dropFirst())
        if args == ["--help"] {
            print("Psst Contacts helper: status | authorize | lookup (bounded JSON on stdin)")
            return
        }
        guard args.count == 1 else { emit(Response(status: "unavailable")); return }
        do {
            switch args[0] {
            case "status": emit(Response(status: status()))
            case "authorize":
                if status() == "permission_required" {
                    _ = try await CNContactStore().requestAccess(for: .contacts)
                }
                emit(Response(status: status()))
            case "lookup":
                let request = try request() // Validate before touching authorization or Contacts.
                let authorization = status()
                guard authorization == "authorized" || authorization == "limited" else {
                    emit(Response(status: authorization)); return
                }
                let store = CNContactStore()
                let batch = ContactLookup.resolve(request.handles, store: store, region: Locale.current.region?.identifier)
                emit(Response(status: authorization, names: batch.names, failure: batch.failure, outcomes: batch.outcomes))
            default: emit(Response(status: "unavailable"))
            }
        } catch { emit(Response(status: "unavailable", failure: "request_invalid")) }
    }
}
