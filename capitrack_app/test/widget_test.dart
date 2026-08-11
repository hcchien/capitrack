import 'package:capitrack_app/main.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('shows the CapiTrack shell', (tester) async {
    await tester.pumpWidget(const CapiTrackApp());
    await tester.pump();
    expect(find.text('CAPITRACK'), findsOneWidget);
    expect(find.text('總覽'), findsWidgets);
  });
}
